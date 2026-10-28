package order

import (
	"fmt"
)

// transitions is the whole machine. Any (state, event) pair not listed is illegal.
var transitions = map[transitionKey]State{
	{StateInitialized, EventAuthorize}:      StatePaymentAuthorized,
	{StateInitialized, EventReject}:         StateRejected,
	{StatePaymentAuthorized, EventFulfill}:  StateComplete,
	{StatePaymentAuthorized, EventCancel}:   StateCancelled,
	{StatePaymentAuthorized, EventEscalate}: StateNeedsAttention,
}

// Next returns the State that an Event leads to from the given state.
// the caller decides whether to apply the result.
func Next(from State, ev Event) (State, error) {
	to, ok := transitions[transitionKey{from, ev}]
	if !ok {
		return from, fmt.Errorf("%w: %s from %s", ErrInvalidTransition, ev, from)
	}
	return to, nil
}

// IsTerminal reports whether no event can move an order out of s.
func (s State) IsTerminal() bool {
	for k := range transitions {
		if k.from == s {
			return false
		}
	}
	return true
}
