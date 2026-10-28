package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/five07/gt-order-fsm/internal/util"
)

// Stub is a fake payment gateway for tests and local runs. It fails on
// the test cards (TestCardDecline, TestCardVoidFails). AuthorizeErr and
// VoidErr, when set, take precedence for every card.
//
// It satisfies order.PaymentGateway. That is checked in the order tests,
// since payment cannot import order.
type Stub struct {
	AuthorizeErr error
	VoidErr      error

	mu             sync.Mutex
	authorizeCalls int
	voided         []Authorization
	cards          map[string]string // authorization ID -> card, for Void
}

func (s *Stub) Authorize(_ context.Context, _ string, _ Money, card string) (Authorization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.authorizeCalls++
	switch {
	case s.AuthorizeErr != nil:
		return Authorization{Status: Failure}, s.AuthorizeErr
	case card == TestCardDecline:
		return Authorization{Status: Failure}, fmt.Errorf("%w: simulated by test card %s", ErrDeclined, card)
	}

	auth := Authorization{ID: util.NewID(), Status: Success}
	if s.cards == nil {
		s.cards = make(map[string]string)
	}
	s.cards[auth.ID] = card
	return auth, nil
}

func (s *Stub) Void(_ context.Context, auth Authorization) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.voided = append(s.voided, auth)
	switch {
	case s.VoidErr != nil:
		return s.VoidErr
	case s.cards[auth.ID] == TestCardVoidFails:
		return errors.New("simulated by test card " + TestCardVoidFails)
	}
	return nil
}

// AuthorizeCalls reports how many times Authorize was called.
func (s *Stub) AuthorizeCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authorizeCalls
}

// Voided returns every authorization passed to Void, in call order.
func (s *Stub) Voided() []Authorization {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Authorization(nil), s.voided...)
}
