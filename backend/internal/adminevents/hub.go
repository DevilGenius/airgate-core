package adminevents

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

const (
	TypeAccountCapacityChanged = "account_capacity.changed"
	TypeMonitorChanged         = "monitor.changed"
)

const (
	defaultSubscriberBuffer = 64
)

// Event is one admin-only server event delivered over SSE.
type Event struct {
	Type               string `json:"type"`
	TS                 string `json:"ts"`
	AccountID          int    `json:"account_id,omitempty"`
	CurrentConcurrency int    `json:"current_concurrency"`
	Reason             string `json:"reason,omitempty"`
}

// Hub fans out admin events to connected SSE clients. Publishing never blocks
// request hot paths; a slow subscriber drops older buffered events.
type Hub struct {
	mu      sync.RWMutex
	nextID  atomic.Uint64
	buffer  int
	subs    map[uint64]chan Event
	now     func() time.Time
	dropped atomic.Int64
	pushed  atomic.Int64
}

// NewHub creates an admin event hub.
func NewHub(buffer int) *Hub {
	if buffer <= 0 {
		buffer = defaultSubscriberBuffer
	}
	return &Hub{
		buffer: buffer,
		subs:   make(map[uint64]chan Event),
		now:    time.Now,
	}
}

// Subscribe registers one event subscriber. The returned cancel function is
// idempotent and should be called when the client disconnects.
func (h *Hub) Subscribe(ctx context.Context) (<-chan Event, func()) {
	if h == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	id := h.nextID.Add(1)
	ch := make(chan Event, h.buffer)

	h.mu.Lock()
	h.subs[id] = ch
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			close(ch)
			h.mu.Unlock()
		})
	}
	if ctx != nil {
		go func() {
			<-ctx.Done()
			cancel()
		}()
	}
	return ch, cancel
}

// PublishAccountCapacityChanged reports one account's current concurrency.
func (h *Hub) PublishAccountCapacityChanged(accountID int, currentConcurrency int) {
	if h == nil || accountID <= 0 {
		return
	}
	h.Publish(Event{
		Type:               TypeAccountCapacityChanged,
		AccountID:          accountID,
		CurrentConcurrency: currentConcurrency,
	})
}

// PublishMonitorChanged reports that monitor list/summary data changed.
func (h *Hub) PublishMonitorChanged(reason string) {
	if h == nil {
		return
	}
	h.Publish(Event{
		Type:   TypeMonitorChanged,
		Reason: reason,
	})
}

// Publish sends an event to all current subscribers without blocking.
func (h *Hub) Publish(event Event) {
	if h == nil || event.Type == "" {
		return
	}
	if event.TS == "" {
		event.TS = h.now().UTC().Format(time.RFC3339Nano)
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, ch := range h.subs {
		if h.trySend(ch, event) {
			h.pushed.Add(1)
		} else {
			h.dropped.Add(1)
		}
	}
}

func (h *Hub) trySend(ch chan Event, event Event) bool {
	select {
	case ch <- event:
		return true
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- event:
		return true
	default:
		return false
	}
}
