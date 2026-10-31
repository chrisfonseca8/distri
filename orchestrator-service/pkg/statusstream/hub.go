// Package statusstream provides bounded, in-process checkout status notifications
// for the current single-replica orchestrator deployment.
package statusstream

import (
	"errors"
	"sync"
)

var ErrSubscriberLimit = errors.New("checkout status subscriber limit reached")

// Hub fans a change notification out to subscribers for one order. Notifications
// carry no state: the database remains authoritative and the SSE handler reloads
// the latest snapshot. Buffered size one coalesces repeated transitions for a slow
// client without blocking saga processing.
type Hub struct {
	mu          sync.Mutex
	byOrder     map[string]map[uint64]chan struct{}
	nextID      uint64
	subscribers int
	max         int
}

// NewHub creates a hub with a process-wide subscriber limit.
func NewHub(maxSubscribers int) *Hub {
	if maxSubscribers <= 0 {
		maxSubscribers = 10_000
	}
	return &Hub{byOrder: make(map[string]map[uint64]chan struct{}), max: maxSubscribers}
}

// Subscribe registers one listener and returns an idempotent cancellation function.
func (h *Hub) Subscribe(orderID string) (<-chan struct{}, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subscribers >= h.max {
		return nil, nil, ErrSubscriberLimit
	}
	h.nextID++
	id := h.nextID
	ch := make(chan struct{}, 1)
	listeners := h.byOrder[orderID]
	if listeners == nil {
		listeners = make(map[uint64]chan struct{})
		h.byOrder[orderID] = listeners
	}
	listeners[id] = ch
	h.subscribers++

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			listeners := h.byOrder[orderID]
			if _, exists := listeners[id]; !exists {
				return
			}
			delete(listeners, id)
			h.subscribers--
			if len(listeners) == 0 {
				delete(h.byOrder, orderID)
			}
		})
	}
	return ch, cancel, nil
}

// Notify tells every local subscriber that orderID changed. It never blocks.
func (h *Hub) Notify(orderID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.byOrder[orderID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Subscribers returns the current process-wide count for telemetry and tests.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subscribers
}
