package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/five07/gt-order-fsm/internal/order"
)

// Memory is an in-memory order store. It clones on Save and on Get, so
// callers can change what they hold without affecting what is stored.
type Memory struct {
	mu     sync.RWMutex
	orders map[string]*order.Order
}

var _ order.Store = (*Memory)(nil)

// NewMemory returns an empty Memory store.
func NewMemory() *Memory {
	return &Memory{orders: make(map[string]*order.Order)}
}

// Get fetches a stored order as a clone
func (m *Memory) Get(_ context.Context, id string) (*order.Order, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	o, ok := m.orders[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", order.ErrNotFound, id)
	}
	return o.Clone(), nil
}

// Save saves an order clone
func (m *Memory) Save(_ context.Context, o *order.Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.orders[o.ID] = o.Clone()
	return nil
}
