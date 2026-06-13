package monitor

import (
	"testing"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

func TestNormalizeInputDowngradesSingleAccountSeverity(t *testing.T) {
	accountID := 4114
	service := NewService(nil)

	event := service.normalizeInput(monitoring.EventInput{
		Type:        monitoring.TypeUpstreamAccountError,
		Severity:    monitoring.SeverityCritical,
		SubjectType: monitoring.SubjectAccount,
		AccountID:   &accountID,
		SubjectID:   "4114",
	}).Event

	if event.Severity != monitoring.SeverityWarning {
		t.Fatalf("severity = %q, want warning", event.Severity)
	}
}

func TestNormalizeInputKeepsNonAccountSeverity(t *testing.T) {
	service := NewService(nil)

	event := service.normalizeInput(monitoring.EventInput{
		Type:        monitoring.TypeSchedulerError,
		Severity:    monitoring.SeverityError,
		SubjectType: monitoring.SubjectScheduler,
		SubjectID:   "openai",
	}).Event

	if event.Severity != monitoring.SeverityError {
		t.Fatalf("severity = %q, want error", event.Severity)
	}
}
