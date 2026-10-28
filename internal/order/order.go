package order

import (
	"errors"
	"slices"
	"time"

	"github.com/five07/gt-order-fsm/internal/payment"
)

// New returns an order in StateInitialized with its creation recorded.
func New(id string, amount payment.Money, card string, at time.Time) *Order {
	at = at.UTC()

	return &Order{
		ID:         id,
		Amount:     amount,
		CardNumber: card,
		State:      StateInitialized,
		History: []HistoryEntry{
			{To: StateInitialized, At: at},
		},
		CreatedAt: at,
		UpdatedAt: at,
	}
}

// Clone returns a deep copy of the order.
func (o *Order) Clone() *Order {
	c := *o
	c.History = slices.Clone(o.History)
	if o.Authorization != nil {
		auth := *o.Authorization
		c.Authorization = &auth
	}
	return &c
}

// apply moves the order to the state ev leads to and records it with a note
func (o *Order) apply(ev Event, at time.Time, note string, errs ...error) error {
	to, err := Next(o.State, ev)
	if err != nil {
		return err
	}
	at = at.UTC()
	o.History = append(o.History, HistoryEntry{
		From:  o.State,
		To:    to,
		Event: ev,
		At:    at,
		Note:  note,
		Err:   errors.Join(errs...),
	})
	o.State = to
	o.UpdatedAt = at
	return nil
}
