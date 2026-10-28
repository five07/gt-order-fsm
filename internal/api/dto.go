package api

import (
	"time"

	"github.com/five07/gt-order-fsm/internal/order"
)

type createOrderRequest struct {
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	CardNumber  string `json:"card_number"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type orderResponse struct {
	ID              string         `json:"id"`
	State           order.State    `json:"state"`
	AmountCents     int64          `json:"amount_cents"`
	Currency        string         `json:"currency"`
	CardLast4       string         `json:"card_last4"`
	AuthorizationID string         `json:"authorization_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	History         []historyEntry `json:"history"`
}

type historyEntry struct {
	From   order.State `json:"from,omitempty"`
	To     order.State `json:"to"`
	Event  order.Event `json:"event,omitempty"`
	At     time.Time   `json:"at"`
	Note   string      `json:"note,omitempty"`
	Errors []string    `json:"errors,omitempty"`
}

func newOrderResponse(o *order.Order) orderResponse {
	resp := orderResponse{
		ID:          o.ID,
		State:       o.State,
		AmountCents: o.Amount.Amount,
		Currency:    o.Amount.Currency.String(),
		CardLast4:   last4(o.CardNumber),
		CreatedAt:   o.CreatedAt,
		UpdatedAt:   o.UpdatedAt,
		History:     make([]historyEntry, 0, len(o.History)),
	}
	if o.Authorization != nil {
		resp.AuthorizationID = o.Authorization.ID
	}
	for _, h := range o.History {
		resp.History = append(resp.History, historyEntry{
			From:   h.From,
			To:     h.To,
			Event:  h.Event,
			At:     h.At,
			Note:   h.Note,
			Errors: errorStrings(h.Err),
		})
	}
	return resp
}

// last4 masks a card number. The full number is never returned.
func last4(card string) string {
	if len(card) <= 4 {
		return card
	}
	return card[len(card)-4:]
}

// errorStrings splits a joined error
func errorStrings(err error) []string {
	if err == nil {
		return nil
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []string{err.Error()}
	}
	var out []string
	for _, e := range joined.Unwrap() {
		out = append(out, e.Error())
	}
	return out
}
