package fulfillment

import (
	"context"
	"errors"
	"sync"

	"github.com/five07/gt-order-fsm/internal/order"
	"github.com/five07/gt-order-fsm/internal/payment"
)

// Stub is a scripted Fulfiller for tests and local runs. It fails for orders
// paid with payment.TestCardFulfillmentFails or payment.TestCardVoidFails.
// Err, when set, takes precedence for every order.
type Stub struct {
	Err error

	mu    sync.Mutex
	calls int
}

var _ order.Fulfiller = (*Stub)(nil)

func (s *Stub) Complete(_ context.Context, o order.Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++
	switch {
	case s.Err != nil:
		return s.Err
	case o.CardNumber == payment.TestCardFulfillmentFails, o.CardNumber == payment.TestCardVoidFails:
		return errors.New("simulated by test card " + o.CardNumber)
	}
	return nil
}

// Calls reports how many times Complete was called.
func (s *Stub) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}
