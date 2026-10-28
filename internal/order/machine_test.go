package order

import (
	"errors"
	"testing"
)

var allStates = []State{
	StateInitialized,
	StatePaymentAuthorized,
	StateComplete,
	StateRejected,
	StateCancelled,
	StateNeedsAttention,
}

var allEvents = []Event{
	EventAuthorize,
	EventReject,
	EventFulfill,
	EventCancel,
	EventEscalate,
}

// TestNext checks every (state, event) pair. The expected transitions are
// written out independently of the table so a typo in either one fails.
func TestNext(t *testing.T) {
	legal := map[State]map[Event]State{
		StateInitialized: {
			EventAuthorize: StatePaymentAuthorized,
			EventReject:    StateRejected,
		},
		StatePaymentAuthorized: {
			EventFulfill:  StateComplete,
			EventCancel:   StateCancelled,
			EventEscalate: StateNeedsAttention,
		},
	}

	for _, from := range allStates {
		for _, ev := range allEvents {
			t.Run(string(from)+"/"+string(ev), func(t *testing.T) {
				got, err := Next(from, ev)

				// fmt.Println(from, ev)

				want, ok := legal[from][ev]
				if ok {
					if err != nil {
						t.Fatalf("Next(%s, %s) error = %v, want nil", from, ev, err)
					}
					if got != want {
						t.Fatalf("Next(%s, %s) = %s, want %s", from, ev, got, want)
					}
					return
				}

				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("Next(%s, %s) error = %v, want ErrInvalidTransition", from, ev, err)
				}
				if got != from {
					t.Fatalf("Next(%s, %s) = %s, want unchanged %s", from, ev, got, from)
				}
			})
		}
	}
}

func TestIsTerminal(t *testing.T) {
	tests := map[State]bool{
		StateInitialized:       false,
		StatePaymentAuthorized: false,
		StateComplete:          true,
		StateRejected:          true,
		StateCancelled:         true,
		StateNeedsAttention:    true,
	}

	for s, want := range tests {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, want %v", s, got, want)
		}
	}
}
