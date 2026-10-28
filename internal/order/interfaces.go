package order

import (
	"context"

	"github.com/five07/gt-order-fsm/internal/payment"
)

// Everything the order service needs from the outside world. The packages
// that implement these (store, fulfillment, payment) depend on order, never
// the reverse.

// Store persists orders.
// Get must return a copy, including History,
// so callers never share memory with the stored order.
type Store interface {
	Get(ctx context.Context, id string) (*Order, error)
	Save(ctx context.Context, o *Order) error
}

// PaymentGateway holds and releases funds for an order. Authorize returns an
// error wrapping payment.ErrDeclined when the payment is refused.
type PaymentGateway interface {
	Authorize(ctx context.Context, orderID string, amount payment.Money, card string) (payment.Authorization, error)
	Void(ctx context.Context, auth payment.Authorization) error
}

// Fulfiller delivers what was ordered (e.g issuing tickets, shipping merch).
type Fulfiller interface {
	Complete(ctx context.Context, o Order) error
}
