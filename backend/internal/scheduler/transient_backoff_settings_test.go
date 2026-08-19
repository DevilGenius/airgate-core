package scheduler

import (
	"testing"
	"time"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
)

func TestSchedulerFamilyTransientBackoffSettingsUpdate(t *testing.T) {
	s := &Scheduler{state: NewStateMachine(nil, nil)}
	item := appsettings.ItemInput{
		Key:   FamilyTransientBackoffSettingKey,
		Value: "12",
		Group: FamilyTransientBackoffSettingsGroup,
	}
	if err := s.ValidateSettingsUpdate([]appsettings.ItemInput{item}); err != nil {
		t.Fatalf("ValidateSettingsUpdate() error = %v", err)
	}
	s.ApplySettingsUpdate([]appsettings.ItemInput{item})

	policy := s.state.familyTransientBackoffPolicy()
	if policy.Seconds != 12 || policy.FixedDuration() != 12*time.Second {
		t.Fatalf("policy = %+v, want 12 seconds", policy)
	}
}

func TestSchedulerFamilyTransientBackoffSettingsSpecialValues(t *testing.T) {
	for _, value := range []string{"-1", "0", "1"} {
		if _, err := parseFamilyTransientBackoffSeconds(value); err != nil {
			t.Fatalf("parseFamilyTransientBackoffSeconds(%q) error = %v", value, err)
		}
	}
}

func TestSchedulerFamilyTransientBackoffSettingsRejectInvalidValues(t *testing.T) {
	s := &Scheduler{state: NewStateMachine(nil, nil)}
	tests := []appsettings.ItemInput{
		{Key: FamilyTransientBackoffSettingKey, Value: "-2", Group: FamilyTransientBackoffSettingsGroup},
		{Key: FamilyTransientBackoffSettingKey, Value: "fixed", Group: FamilyTransientBackoffSettingsGroup},
		{Key: FamilyTransientBackoffSettingKey, Value: "0", Group: "system"},
	}
	for _, item := range tests {
		if err := s.ValidateSettingsUpdate([]appsettings.ItemInput{item}); err == nil {
			t.Fatalf("ValidateSettingsUpdate(%+v) expected error", item)
		}
	}
}
