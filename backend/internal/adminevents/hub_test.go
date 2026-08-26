package adminevents

import (
	"context"
	"testing"
	"time"
)

func TestNilHubSubscribeReturnsClosedChannel(t *testing.T) {
	var hub *Hub
	ch, cancel, baseline := hub.SubscribeWithSequence(context.Background())
	cancel()
	if baseline != 0 {
		t.Fatalf("nil hub baseline = %d, want 0", baseline)
	}

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
	if event.Seq != 1 {
		t.Fatalf("event seq = %d, want 1", event.Seq)
	}
	if event.TS != now.Format(time.RFC3339Nano) {
		t.Fatalf("event timestamp = %q", event.TS)
	}
	if pushed := hub.pushed.Load(); pushed != 1 {
		t.Fatalf("pushed = %d, want 1", pushed)
	}

	until := now.Add(time.Hour)
	hub.PublishAccountStateChanged(7, "rate_limited", &until, "limited")
	statusEvent := <-ch
	if statusEvent.Type != TypeAccountStatusChanged || statusEvent.AccountID != 7 ||
		statusEvent.Seq != 2 ||
		statusEvent.AccountState != "rate_limited" || statusEvent.StateUntil == nil ||
		*statusEvent.StateUntil != until.Format(time.RFC3339Nano) || statusEvent.ErrorMsg == nil || *statusEvent.ErrorMsg != "limited" {
		t.Fatalf("status event = %#v", statusEvent)
	}

	hub.PublishAccountFamilyCooldownChanged(7, "upsert", "gpt-5.6-sol", &until, "subscription", 90000)
	cooldownEvent := <-ch
	if cooldownEvent.Type != TypeAccountStatusChanged || cooldownEvent.FamilyAction != "upsert" ||
		cooldownEvent.Seq != 3 ||
		cooldownEvent.Family != "gpt-5.6-sol" || cooldownEvent.FamilyUntil != until.Format(time.RFC3339Nano) ||
		cooldownEvent.FamilyDurationMs != 90000 {
		t.Fatalf("cooldown event = %#v", cooldownEvent)
	}

	cancel()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel remains open after cancel")
	}
}

func TestHubSubscribeWithSequenceReturnsAtomicBaseline(t *testing.T) {
	hub := NewHub(1)
	hub.Publish(Event{Type: TypeMonitorChanged})

	ch, cancel, baseline := hub.SubscribeWithSequence(context.Background())
	defer cancel()
	if baseline != 1 {
		t.Fatalf("baseline = %d, want 1", baseline)
	}

	hub.Publish(Event{Type: TypeMonitorChanged})
	event := <-ch
	if event.Seq != 2 {
		t.Fatalf("event seq = %d, want 2", event.Seq)
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

func TestHubBroadcastMonitorChangedAndIgnoresInvalidEvents(t *testing.T) {
	hub := NewHub(1)
	ch, cancel := hub.Subscribe(context.Background())
	defer cancel()

	var nilHub *Hub
	nilHub.Publish(Event{Type: "ignored"})
	nilHub.BroadcastMonitorChanged("ignored")
	nilHub.PublishAccountCapacityChanged(1, 1)
	nilHub.PublishAccountStateChanged(1, "active", nil, "")
	nilHub.PublishAccountFamilyCooldownChanged(1, "clear", "", nil, "", 0)

	hub.Publish(Event{})
	hub.PublishAccountCapacityChanged(0, 1)
	hub.PublishAccountStateChanged(0, "active", nil, "")
	hub.PublishAccountFamilyCooldownChanged(0, "clear", "", nil, "", 0)
	hub.PublishAccountFamilyCooldownChanged(1, "invalid", "", nil, "", 0)
	hub.BroadcastMonitorChanged("refresh")

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
	if event.Reason != "new" || event.Seq != 2 {
		t.Fatalf("event reason = %q, want newest event", event.Reason)
	}
	if pushed := hub.pushed.Load(); pushed != 2 {
		t.Fatalf("pushed = %d, want 2", pushed)
	}
	if dropped := hub.dropped.Load(); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
}
