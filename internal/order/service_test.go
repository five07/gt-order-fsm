package order_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/five07/gt-order-fsm/internal/fulfillment"
	"github.com/five07/gt-order-fsm/internal/order"
	"github.com/five07/gt-order-fsm/internal/payment"
	"github.com/five07/gt-order-fsm/internal/store"
	"golang.org/x/text/currency"
)

var testAmount = payment.Money{Amount: 12500, Currency: currency.USD}

const testCard = "4242424242424242"

var _ order.PaymentGateway = (*payment.Stub)(nil)

// fakeClock advances one second on every call, so history timestamps are
// predictable and strictly increasing.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Second)
	return c.t
}

func newTestService(pay order.PaymentGateway, ful *fulfillment.Stub) (*order.Service, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)}
	log := slog.New(slog.DiscardHandler)
	return order.NewService(store.NewMemory(), pay, ful, log, clock.Now), clock
}

func TestAdvanceScenarios(t *testing.T) {
	errFulfill := errors.New("ticketing unavailable")
	errVoid := errors.New("gateway timeout")

	tests := []struct {
		name         string
		authorizeErr error
		completeErr  error
		voidErr      error

		wantStates   []order.State // after each Advance
		wantErrs     []error       // all must match the last history entry's Err
		wantFulfills int
		wantVoids    int
	}{
		{
			name:         "happy path",
			wantStates:   []order.State{order.StatePaymentAuthorized, order.StateComplete},
			wantFulfills: 1,
		},
		{
			name:         "payment declined",
			authorizeErr: fmt.Errorf("card refused: %w", payment.ErrDeclined),
			wantStates:   []order.State{order.StateRejected},
			wantErrs:     []error{payment.ErrDeclined},
		},
		{
			name:         "fulfillment fails, void succeeds",
			completeErr:  errFulfill,
			wantStates:   []order.State{order.StatePaymentAuthorized, order.StateCancelled},
			wantErrs:     []error{errFulfill},
			wantFulfills: 1,
			wantVoids:    1,
		},
		{
			name:         "fulfillment fails, void fails",
			completeErr:  errFulfill,
			voidErr:      errVoid,
			wantStates:   []order.State{order.StatePaymentAuthorized, order.StateNeedsAttention},
			wantErrs:     []error{errFulfill, errVoid},
			wantFulfills: 1,
			wantVoids:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pay := &payment.Stub{AuthorizeErr: tt.authorizeErr, VoidErr: tt.voidErr}
			ful := &fulfillment.Stub{Err: tt.completeErr}
			svc, _ := newTestService(pay, ful)

			o, err := svc.Create(ctx, testAmount, testCard)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			for i, want := range tt.wantStates {
				o, err = svc.Advance(ctx, o.ID)
				if err != nil {
					t.Fatalf("Advance #%d: %v", i+1, err)
				}
				if o.State != want {
					t.Fatalf("Advance #%d state = %s, want %s", i+1, o.State, want)
				}
			}

			// Creation plus one entry per advance, each later than the last.
			if got, want := len(o.History), len(tt.wantStates)+1; got != want {
				t.Fatalf("history has %d entries, want %d", got, want)
			}
			for i := 1; i < len(o.History); i++ {
				if !o.History[i].At.After(o.History[i-1].At) {
					t.Errorf("history[%d].At = %v, not after %v", i, o.History[i].At, o.History[i-1].At)
				}
			}

			last := o.History[len(o.History)-1]
			if len(tt.wantErrs) == 0 && last.Err != nil {
				t.Errorf("last history Err = %v, want nil", last.Err)
			}
			for _, want := range tt.wantErrs {
				if !errors.Is(last.Err, want) {
					t.Errorf("last history Err = %v, want it to match %v", last.Err, want)
				}
			}

			if got := ful.Calls(); got != tt.wantFulfills {
				t.Errorf("Complete called %d times, want %d", got, tt.wantFulfills)
			}
			voided := pay.Voided()
			if len(voided) != tt.wantVoids {
				t.Fatalf("Void called %d times, want %d", len(voided), tt.wantVoids)
			}
			if tt.wantVoids > 0 && voided[0].ID != o.Authorization.ID {
				t.Errorf("voided authorization %q, want %q", voided[0].ID, o.Authorization.ID)
			}

			// The stored order matches what Advance returned.
			stored, err := svc.Get(ctx, o.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if stored.State != o.State || len(stored.History) != len(o.History) {
				t.Errorf("stored order is %s with %d entries, want %s with %d",
					stored.State, len(stored.History), o.State, len(o.History))
			}
		})
	}
}

func TestAdvanceTerminalState(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(&payment.Stub{}, &fulfillment.Stub{})

	o, _ := svc.Create(ctx, testAmount, testCard)
	svc.Advance(ctx, o.ID)
	o, _ = svc.Advance(ctx, o.ID)
	if o.State != order.StateComplete {
		t.Fatalf("setup: state = %s, want %s", o.State, order.StateComplete)
	}

	_, err := svc.Advance(ctx, o.ID)
	if !errors.Is(err, order.ErrTerminalState) {
		t.Fatalf("Advance error = %v, want order.ErrTerminalState", err)
	}

	stored, _ := svc.Get(ctx, o.ID)
	if len(stored.History) != len(o.History) {
		t.Errorf("history grew from %d to %d entries", len(o.History), len(stored.History))
	}
}

func TestAdvanceTransientAuthorizeError(t *testing.T) {
	ctx := context.Background()
	pay := &payment.Stub{AuthorizeErr: errors.New("connection reset")}
	svc, _ := newTestService(pay, &fulfillment.Stub{})

	o, _ := svc.Create(ctx, testAmount, testCard)

	_, err := svc.Advance(ctx, o.ID)
	if !errors.Is(err, order.ErrUpstream) {
		t.Fatalf("Advance error = %v, want order.ErrUpstream", err)
	}

	stored, _ := svc.Get(ctx, o.ID)
	if stored.State != order.StateInitialized || len(stored.History) != 1 {
		t.Fatalf("order is %s with %d entries, want unchanged", stored.State, len(stored.History))
	}

	// Retrying once the gateway recovers succeeds.
	pay.AuthorizeErr = nil
	o, err = svc.Advance(ctx, o.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if o.State != order.StatePaymentAuthorized {
		t.Errorf("retry state = %s, want %s", o.State, order.StatePaymentAuthorized)
	}
}

func TestAdvanceNotFound(t *testing.T) {
	svc, _ := newTestService(&payment.Stub{}, &fulfillment.Stub{})

	_, err := svc.Advance(context.Background(), "missing")
	if !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("Advance error = %v, want order.ErrNotFound", err)
	}
}

// blockingGateway holds Authorize open until release is closed, so a test can
// fire a second Advance while the first is mid-call.
type blockingGateway struct {
	*payment.Stub
	entered chan struct{}
	release chan struct{}
}

func (g *blockingGateway) Authorize(ctx context.Context, id string, amount payment.Money, card string) (payment.Authorization, error) {
	close(g.entered)
	<-g.release
	return g.Stub.Authorize(ctx, id, amount, card)
}

func TestAdvanceConcurrent(t *testing.T) {
	ctx := context.Background()
	pay := &blockingGateway{
		Stub:    &payment.Stub{},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc, _ := newTestService(pay, &fulfillment.Stub{})
	o, _ := svc.Create(ctx, testAmount, testCard)

	firstErr := make(chan error, 1)
	go func() {
		_, err := svc.Advance(ctx, o.ID)
		firstErr <- err
	}()
	<-pay.entered

	if _, err := svc.Advance(ctx, o.ID); !errors.Is(err, order.ErrAdvanceInProgress) {
		t.Errorf("second Advance error = %v, want order.ErrAdvanceInProgress", err)
	}

	close(pay.release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first Advance: %v", err)
	}
	if got := pay.AuthorizeCalls(); got != 1 {
		t.Errorf("Authorize called %d times, want 1", got)
	}
}

var errSave = errors.New("disk full")

// flakyStore is a Memory store whose Save can be switched to fail.
type flakyStore struct {
	*store.Memory
	failSave bool
}

func (s *flakyStore) Save(ctx context.Context, o *order.Order) error {
	if s.failSave {
		return errSave
	}
	return s.Memory.Save(ctx, o)
}

func TestNewServiceDefaults(t *testing.T) {
	before := time.Now()
	svc := order.NewService(store.NewMemory(), &payment.Stub{}, &fulfillment.Stub{}, nil, nil)

	o, err := svc.Create(context.Background(), testAmount, testCard)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if o.CreatedAt.Before(before.Add(-time.Second)) || o.CreatedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("CreatedAt = %v, want about now from the default clock", o.CreatedAt)
	}
}

func TestCreateSaveError(t *testing.T) {
	st := &flakyStore{Memory: store.NewMemory(), failSave: true}
	svc := order.NewService(st, &payment.Stub{}, &fulfillment.Stub{}, slog.New(slog.DiscardHandler), nil)

	if _, err := svc.Create(context.Background(), testAmount, testCard); !errors.Is(err, errSave) {
		t.Fatalf("Create error = %v, want errSave", err)
	}
}

func TestAdvanceSaveError(t *testing.T) {
	ctx := context.Background()
	st := &flakyStore{Memory: store.NewMemory()}
	svc := order.NewService(st, &payment.Stub{}, &fulfillment.Stub{}, slog.New(slog.DiscardHandler), nil)

	o, _ := svc.Create(ctx, testAmount, testCard)
	st.failSave = true

	if _, err := svc.Advance(ctx, o.ID); !errors.Is(err, errSave) {
		t.Fatalf("Advance error = %v, want errSave", err)
	}
	stored, _ := svc.Get(ctx, o.ID)
	if stored.State != order.StateInitialized {
		t.Errorf("stored state = %s, want %s", stored.State, order.StateInitialized)
	}
}

func TestAdvanceAuthorizedWithoutAuthorization(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	ful := &fulfillment.Stub{}
	svc := order.NewService(st, &payment.Stub{}, ful, slog.New(slog.DiscardHandler), nil)

	// A corrupt record: authorized state but no authorization to void.
	o := order.New("ord_corrupt", testAmount, testCard, time.Now())
	o.State = order.StatePaymentAuthorized
	st.Save(ctx, o)

	if _, err := svc.Advance(ctx, o.ID); err == nil {
		t.Fatal("Advance error = nil, want an error")
	}
	if got := ful.Calls(); got != 0 {
		t.Errorf("Complete called %d times, want 0", got)
	}
}
