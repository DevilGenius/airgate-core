package adminevents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServiceKeepsAccountFieldsWithoutModelStatistics(t *testing.T) {
	service := NewService(8)
	ch, cancel, _ := service.SubscribeWithSequence(nil)
	defer cancel()
	zero := 0
	no := false
	service.PublishAccountChanged(7, AccountPatch{MaxConcurrency: &zero, Priority: &zero, SchedulingWeight: &zero, Cognition: &CognitionResult{Degraded: &no}})
	service.PublishAccountStateChanged(7, "disabled", nil, "manual")
	threshold := .9
	service.PublishAccountChanged(7, AccountPatch{ModelDowngradeThreshold: &threshold})
	threshold = 0
	service.PublishAccountChanged(7, AccountPatch{ModelDowngradeThreshold: &threshold})
	service.flush()
	event := nextEvent(t, ch)
	if event.ModelDowngradeThreshold == nil || *event.ModelDowngradeThreshold != 0 {
		t.Fatal("missing final threshold")
	}
	if event.MaxConcurrency == nil || *event.MaxConcurrency != 0 || event.Priority == nil || event.SchedulingWeight == nil || event.Cognition == nil || *event.Cognition.Degraded || event.AccountState != "disabled" {
		t.Fatalf("missing account fields: %+v", event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "model_demotions") || strings.Contains(string(payload), "valid_requests") {
		t.Fatalf("statistics in SSE: %s", payload)
	}
}
