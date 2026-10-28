package order

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/five07/gt-order-fsm/internal/payment"
	"github.com/five07/gt-order-fsm/internal/util"
)

// Service runs each order through its lifecycle. It owns all side effects
// the machine only says which transitions are legal.
type Service struct {
	store     Store
	payments  PaymentGateway
	fulfiller Fulfiller
	log       *slog.Logger
	now       func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per-order; never pruned in this prototype
}

// NewService creates a Service.
func NewService(orders Store, payments PaymentGateway, fulfiller Fulfiller, log *slog.Logger, now func() time.Time) *Service {
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:     orders,
		payments:  payments,
		fulfiller: fulfiller,
		log:       log,
		now:       now,
		locks:     make(map[string]*sync.Mutex),
	}
}

// Create stores a new order in StateInitialized.
func (s *Service) Create(ctx context.Context, amount payment.Money, card string) (*Order, error) {
	o := New(util.NewID(), amount, card, s.now())
	if err := s.store.Save(ctx, o); err != nil {
		return nil, fmt.Errorf("save order: %w", err)
	}
	return o, nil
}

// Get returns the order with the given ID.
func (s *Service) Get(ctx context.Context, id string) (*Order, error) {
	return s.store.Get(ctx, id)
}

// Advance runs the next step for the order's current state. A failure that
// the lifecycle handles (decline, failed fulfillment) is a successful advance
// into rejected, cancelled or needs_attention. An error means the order was
// not changed.
func (s *Service) Advance(ctx context.Context, id string) (*Order, error) {
	unlock, ok := s.tryLock(id)
	if !ok {
		return nil, ErrAdvanceInProgress
	}
	defer unlock()

	o, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if o.State.IsTerminal() {
		return o, ErrTerminalState
	}

	switch o.State {
	case StateInitialized:
		err = s.authorize(ctx, o)
	case StatePaymentAuthorized:
		err = s.fulfill(ctx, o)
	default:
		err = fmt.Errorf("%w: no step for %s", ErrInvalidTransition, o.State)
	}
	if err != nil {
		return nil, err
	}

	if err := s.store.Save(ctx, o); err != nil {
		return nil, fmt.Errorf("save order: %w", err)
	}
	return o, nil
}

// authorize the payment
func (s *Service) authorize(ctx context.Context, o *Order) error {
	auth, err := s.payments.Authorize(ctx, o.ID, o.Amount, o.CardNumber)
	switch {
	case errors.Is(err, payment.ErrDeclined):
		return o.apply(EventReject, s.now(), "payment declined; nothing held", err)
	case err != nil:
		return fmt.Errorf("%w: authorize: %w", ErrUpstream, err)
	}

	o.Authorization = &auth
	return o.apply(EventAuthorize, s.now(), fmt.Sprintf("authorization %s held", auth.ID))
}

// fulfill the order
func (s *Service) fulfill(ctx context.Context, o *Order) error {
	if o.Authorization == nil {
		return fmt.Errorf("order %s is %s with no authorization", o.ID, o.State)
	}

	completeErr := s.fulfiller.Complete(ctx, *o)
	if completeErr == nil {
		return o.apply(EventFulfill, s.now(), "order fulfilled")
	}
	completeErr = fmt.Errorf("fulfillment failed: %w", completeErr)

	voidErr := s.payments.Void(ctx, *o.Authorization)
	if voidErr == nil {
		note := fmt.Sprintf("fulfillment failed; authorization %s voided", o.Authorization.ID)
		return o.apply(EventCancel, s.now(), note, completeErr)
	}
	voidErr = fmt.Errorf("void failed: %w", voidErr)

	s.log.Error("order needs attention",
		"order_id", o.ID,
		"authorization_id", o.Authorization.ID,
		"complete_error", completeErr,
		"void_error", voidErr,
	)
	note := fmt.Sprintf("fulfillment failed; void of authorization %s failed, funds may still be held", o.Authorization.ID)
	return o.apply(EventEscalate, s.now(), note, completeErr, voidErr)
}

// tryLock takes the order's lock and reports false if another Advance call holds it.
func (s *Service) tryLock(id string) (unlock func(), ok bool) {
	s.mu.Lock()
	l, found := s.locks[id]
	if !found {
		l = &sync.Mutex{}
		s.locks[id] = l
	}
	s.mu.Unlock()

	if !l.TryLock() {
		return nil, false
	}
	return l.Unlock, true
}
