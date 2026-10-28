package order

import (
	"errors"
	"time"

	"github.com/five07/gt-order-fsm/internal/payment"
)

var (
	// ErrInvalidTransition is returned when an event is not allowed from a state.
	ErrInvalidTransition = errors.New("invalid transition")
	// ErrNotFound is returned by a Store when no order has the given ID.
	ErrNotFound = errors.New("order not found")
	// ErrTerminalState is returned when advancing an order that has no next step.
	ErrTerminalState = errors.New("order is in a terminal state")
	// ErrAdvanceInProgress is returned when another Advance holds the order.
	ErrAdvanceInProgress = errors.New("advance already in progress")
	// ErrUpstream wraps a transient vendor failure.
	// The order is unchanged and the call can be retried.
	ErrUpstream = errors.New("upstream failure")
)

// The state value for an order
type State string

// The states for an order
const (
	StateInitialized       State = "initialized"
	StatePaymentAuthorized State = "payment_authorized"
	StateComplete          State = "complete"
	StateRejected          State = "rejected"
	StateCancelled         State = "cancelled"
	StateNeedsAttention    State = "needs_attention"
)

// The event value between states
type Event string

// The events for an order
const (
	EventAuthorize Event = "authorize"
	EventReject    Event = "reject"
	EventFulfill   Event = "fulfill"
	EventCancel    Event = "cancel"
	EventEscalate  Event = "escalate"
)

type transitionKey struct {
	from  State
	event Event
}

// Order is a checkout order and its lifecycle.
type Order struct {
	ID     string
	Amount payment.Money
	State  State

	// CardNumber is the card the order is paid with.
	// This exists to supply failure card numbers to simulate failed states from the API
	// It's never returned in full from the API.
	CardNumber string

	// Authorization is only set once payment is authorized and is nil before then.
	Authorization *payment.Authorization

	History   []HistoryEntry
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HistoryEntry records one state change.
type HistoryEntry struct {
	From  State
	To    State
	Event Event
	At    time.Time
	Note  string
	Err   error
}
