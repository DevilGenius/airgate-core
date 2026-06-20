package adminevents

import (
	"context"
	"testing"
	"time"
)

func TestNilHubSubscribeReturnsClosedChannel(t *testing.T) {
	var hub *Hub
	ch, cancel := hub.Subscribe(context.Background())
	cancel()

	if _, ok := <-ch; ok {
		t.Fatal("nil hub subscriber channel is open")
	}
}

func TestNewHubDefaultsBuffer(t *testing.T) {
	hub := NewHub(0)
	if hub.buffer != defaultSubscriberBuffer {
		t.Fatalf("buffer = %d, want default %d", hub.buffer, defaultSubscriberBuffer)
	}
}

func TestHubPublishesTimestampedEventsAndCancelIsIdempotent(t *testing.T) {
	hub := NewHub(2)
	now := time.Date(2026, 6, 20, 1, 2, 3, 4, time.UTC)
	hub.now = func() time.Time { return now }

	ch, cancel := hub.Subscribe(context.Background())
	hub.PublishAccountCapacityChanged(7, 3)

	event := <-ch
	if event.Type != TypeAccountCapacityChanged {
		t.Fatalf("event type = %q", event.Type)
	}
	if event.AccountID != 7 || event.CurrentConcurrency != 3 {
		t.Fatalf("event payload = %#v", event)
	}
	if event.TS != now.Format(time.RFC3339Nano) {
		t.Fatalf("event timestamp = %q", event.TS)
	}
	if pushed := hub.pushed.Load(); pushed != 1 {
		t.Fatalf("pushed = %d, want 1", pushed)
	}

	cancel()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel remains open after cancel")
	}
}

func TestHubSubscribeContextCancel(t *testing.T) {
	hub := NewHub(1)
	ctx, cancelContext := context.WithCancel(context.Background())
	ch, _ := hub.Subscribe(ctx)

	cancelContext()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("subscriber channel remains open after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscriber close")
	}
}

func TestHubPublishMonitorChangedAndIgnoresInvalidEvents(t *testing.T) {
	hub := NewHub(1)
	ch, cancel := hub.Subscribe(context.Background())
	defer cancel()

	var nilHub *Hub
	nilHub.Publish(Event{Type: "ignored"})
	nilHub.PublishMonitorChanged("ignored")
	nilHub.PublishAccountCapacityChanged(1, 1)

	hub.Publish(Event{})
	hub.PublishAccountCapacityChanged(0, 1)
	hub.PublishMonitorChanged("refresh")

	event := <-ch
	if event.Type != TypeMonitorChanged || event.Reason != "refresh" {
		t.Fatalf("event = %#v", event)
	}
}

func TestHubDropsWhenSubscriberCannotReceive(t *testing.T) {
	hub := NewHub(1)
	blocked := make(chan Event)

	hub.mu.Lock()
	hub.subs[1] = blocked
	hub.mu.Unlock()

	hub.Publish(Event{Type: TypeMonitorChanged, TS: "preset"})

	if dropped := hub.dropped.Load(); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if pushed := hub.pushed.Load(); pushed != 0 {
		t.Fatalf("pushed = %d, want 0", pushed)
	}
}

func TestHubSlowSubscriberReceivesNewestBufferedEvent(t *testing.T) {
	hub := NewHub(1)
	ch, cancel := hub.Subscribe(context.Background())
	defer cancel()

	hub.Publish(Event{Type: TypeMonitorChanged, Reason: "old", TS: "1"})
	hub.Publish(Event{Type: TypeMonitorChanged, Reason: "new", TS: "2"})

	event := <-ch
	if event.Reason != "new" {
		t.Fatalf("event reason = %q, want newest event", event.Reason)
	}
	if pushed := hub.pushed.Load(); pushed != 2 {
		t.Fatalf("pushed = %d, want 2", pushed)
	}
}
