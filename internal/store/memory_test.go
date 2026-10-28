package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/five07/gt-order-fsm/internal/order"
	"github.com/five07/gt-order-fsm/internal/payment"
	"golang.org/x/text/currency"
)

func newOrder() *order.Order {
	return order.New("ord_1", payment.Money{Amount: 100, Currency: currency.USD}, "4242", time.Now())
}

func TestGetNotFound(t *testing.T) {
	_, err := NewMemory().Get(context.Background(), "missing")
	if !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("Get error = %v, want order.ErrNotFound", err)
	}
}

func TestSaveThenGet(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	o := newOrder()

	if err := m.Save(ctx, o); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := m.Get(ctx, o.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != o.ID || got.State != o.State || len(got.History) != len(o.History) {
		t.Errorf("Get = %+v, want %+v", got, o)
	}
}

func TestStoreDoesNotShareMemory(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	o := newOrder()
	m.Save(ctx, o)

	// Changing the saved value afterwards does not reach the store.
	o.State = order.StateRejected
	o.History[0].To = order.StateRejected

	got, _ := m.Get(ctx, o.ID)
	if got.State != order.StateInitialized || got.History[0].To != order.StateInitialized {
		t.Fatalf("store changed through the saved pointer: %+v", got)
	}

	// Changing a value from Get does not reach the store either.
	got.State = order.StateComplete
	got.History = append(got.History, order.HistoryEntry{To: order.StateComplete})

	again, _ := m.Get(ctx, o.ID)
	if again.State != order.StateInitialized || len(again.History) != 1 {
		t.Errorf("store changed through a returned pointer: %+v", again)
	}
}
