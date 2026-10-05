package adminevents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountPatchCoalescesIndependentFieldsAndExplicitClear(t *testing.T) {
	hub := NewHub(8)
	ch, cancel := hub.Subscribe(nil)
	defer cancel()
	publisher := NewAccountChangePublisher(hub)
	priority, weight, degraded := 50, 0, true
	publisher.PublishAccountChanged(7, AccountPatch{Priority: &priority, Cognition: &CognitionResult{Degraded: &degraded}})
	priority = 99 // queued patches own their values
	publisher.PublishAccountStateChanged(7, "disabled", nil, "manual")
	publisher.PublishAccountChanged(7, AccountPatch{SchedulingWeight: &weight})
	publisher.PublishAccountChanged(7, AccountPatch{Cognition: &CognitionResult{}})
	publisher.flush()
	event := nextEvent(t, ch)
	if event.Type != "account.changed" || event.Priority == nil || *event.Priority != 50 || event.SchedulingWeight == nil || *event.SchedulingWeight != 0 || event.AccountState != "disabled" {
		t.Fatalf("merged event = %+v", event)
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cognition":{"degraded":null}`) {
		t.Fatalf("missing clear: %s", data)
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected event: %+v", extra)
	default:
	}
}
