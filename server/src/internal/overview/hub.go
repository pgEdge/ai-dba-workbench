/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package overview

import "sync"

// Hub is an SSE fan-out hub that manages subscribers and broadcasts
// Overview updates to them based on scope keys.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[*Subscriber]struct{}
}

// Subscriber represents a single SSE client listening for Overview updates.
type Subscriber struct {
	// C receives Overview updates. The channel is buffered (size 1) so a
	// slow reader does not block the broadcaster.
	C        chan *Overview
	scopeKey string
}

// NewHub creates a Hub ready for use.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[*Subscriber]struct{}),
	}
}

// Subscribe registers a new subscriber scoped to scopeKey and returns it.
// An empty scopeKey subscribes to estate-wide broadcasts.
func (h *Hub) Subscribe(scopeKey string) *Subscriber {
	sub := &Subscriber{
		C:        make(chan *Overview, 1),
		scopeKey: scopeKey,
	}
	h.mu.Lock()
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Unsubscribe removes a subscriber from the hub and closes its channel.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	if _, ok := h.subscribers[sub]; ok {
		delete(h.subscribers, sub)
		close(sub.C)
	}
	h.mu.Unlock()
}

// Broadcast sends an Overview to every subscriber whose scope key matches.
// An empty scopeKey targets estate-wide subscribers; a non-empty scopeKey
// targets only subscribers with that exact scope. The send is non-blocking:
// if a subscriber's buffer is full the message is dropped.
func (h *Hub) Broadcast(overview *Overview, scopeKey string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.subscribers {
		if sub.scopeKey != scopeKey {
			continue
		}
		select {
		case sub.C <- overview:
		default:
			// Drop the message for slow clients.
		}
	}
}

// Count returns the number of active subscribers.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
