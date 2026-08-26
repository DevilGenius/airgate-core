package adminevents

import (
	"context"
	"testing"
	"time"
)

func TestCoalescingStatusPublisherKeepsLatestValuesAndClearOrder(t *testing.T) {
	hub := NewHub(8)
	subscriptionCtx, cancelSubscriptionContext := context.WithCancel(context.Background())
	ch, cancelSubscription := hub.Subscribe(subscriptionCtx)
	defer func() {
		cancelSubscription()
		cancelSubscriptionContext()
	}()
	publisher := NewCoalescingStatusPublisher(hub)

	now := time.Date(2026, 7, 27, 1, 2, 3, 0, time.UTC)
	stateUntil := now.Add(time.Hour)
	oldFamilyUntil := now.Add(2 * time.Hour)
	latestFamilyUntil := now.Add(3 * time.Hour)
	publisher.PublishAccountStateChanged(7, "active", nil, "")
	publisher.PublishAccountStateChanged(7, "degraded", &stateUntil, "upstream")
	publisher.PublishAccountFamilyCooldownChanged(7, "upsert", "old-family", &oldFamilyUntil, "old", 1000)
	publisher.PublishAccountFamilyCooldownChanged(7, "clear", "", nil, "", 0)
	publisher.PublishAccountFamilyCooldownChanged(7, "upsert", "gpt-5", &oldFamilyUntil, "old", 2000)
	publisher.PublishAccountFamilyCooldownChanged(7, "upsert", "gpt-5", &latestFamilyUntil, "latest", 3000)

	publisher.flush()

	stateEvent := nextEvent(t, ch)
	if stateEvent.AccountState != "degraded" || stateEvent.StateUntil == nil ||
		*stateEvent.StateUntil != stateUntil.Format(time.RFC3339Nano) || stateEvent.ErrorMsg == nil ||
		*stateEvent.ErrorMsg != "upstream" {
		t.Fatalf("state event = %#v", stateEvent)
	}

	clearEvent := nextEvent(t, ch)
	if clearEvent.FamilyAction != "clear" || clearEvent.AccountID != 7 {
		t.Fatalf("clear event = %#v", clearEvent)
	}

	upsertEvent := nextEvent(t, ch)
	if upsertEvent.FamilyAction != "upsert" || upsertEvent.Family != "gpt-5" ||
		upsertEvent.FamilyUntil != latestFamilyUntil.Format(time.RFC3339Nano) || upsertEvent.FamilyReason != "latest" ||
		upsertEvent.FamilyDurationMs != 3000 {
		t.Fatalf("upsert event = %#v", upsertEvent)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected extra event = %#v", event)
	default:
	}
}

func TestCoalescingStatusPublisherFlushesOnContextCancel(t *testing.T) {
	hub := NewHub(1)
	subscriptionCtx, cancelSubscriptionContext := context.WithCancel(context.Background())
	ch, cancelSubscription := hub.Subscribe(subscriptionCtx)
	defer func() {
		cancelSubscription()
		cancelSubscriptionContext()
	}()
	publisher := NewCoalescingStatusPublisher(hub)
	publisher.interval = time.Hour
	publisher.PublishAccountStateChanged(9, "disabled", nil, "manual")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		publisher.Run(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher did not stop after context cancellation")
	}

	event := nextEvent(t, ch)
	if event.AccountID != 9 || event.AccountState != "disabled" || event.ErrorMsg == nil || *event.ErrorMsg != "manual" {
		t.Fatalf("shutdown event = %#v", event)
	}
}

func nextEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for admin event")
		return Event{}
	}
}
