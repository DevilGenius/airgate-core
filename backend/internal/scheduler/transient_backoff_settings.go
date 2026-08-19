package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
)

const (
	FamilyTransientBackoffSettingsGroup = "scheduling"
	FamilyTransientBackoffSettingKey    = "family_transient_backoff_seconds"
	FamilyTransientBackoffExponential   = int64(-1)

	maxFamilyTransientBackoffSeconds = int64(^uint64(0)>>1) / int64(time.Second)
)

type FamilyTransientBackoffPolicy struct {
	Seconds int64
}

func DefaultFamilyTransientBackoffPolicy() FamilyTransientBackoffPolicy {
	return FamilyTransientBackoffPolicy{Seconds: FamilyTransientBackoffExponential}
}

func (p FamilyTransientBackoffPolicy) FixedDuration() time.Duration {
	if p.Seconds <= 0 {
		return 0
	}
	return time.Duration(p.Seconds) * time.Second
}

func LoadOrInitializeFamilyTransientBackoffPolicy(ctx context.Context, settings *appsettings.Service) (FamilyTransientBackoffPolicy, error) {
	if settings == nil {
		return FamilyTransientBackoffPolicy{}, fmt.Errorf("settings service is unavailable")
	}
	items, err := settings.List(ctx, FamilyTransientBackoffSettingsGroup)
	if err != nil {
		return FamilyTransientBackoffPolicy{}, fmt.Errorf("load family transient backoff setting: %w", err)
	}

	policy := DefaultFamilyTransientBackoffPolicy()
	found := false
	updates := make([]appsettings.ItemInput, 0, 1)
	for _, item := range items {
		if item.Key != FamilyTransientBackoffSettingKey {
			continue
		}
		found = true
		seconds, parseErr := parseFamilyTransientBackoffSeconds(item.Value)
		if parseErr != nil {
			seconds = policy.Seconds
		}
		policy.Seconds = seconds
		normalized := strconv.FormatInt(seconds, 10)
		if parseErr != nil || strings.TrimSpace(item.Value) != normalized || item.Group != FamilyTransientBackoffSettingsGroup {
			updates = append(updates, familyTransientBackoffSettingItem(normalized))
		}
	}
	if !found {
		updates = append(updates, familyTransientBackoffSettingItem(strconv.FormatInt(policy.Seconds, 10)))
	}
	if len(updates) > 0 {
		if err := settings.Update(ctx, updates); err != nil {
			return FamilyTransientBackoffPolicy{}, fmt.Errorf("initialize family transient backoff setting: %w", err)
		}
	}
	return policy, nil
}

func (s *Scheduler) ValidateSettingsUpdate(items []appsettings.ItemInput) error {
	if s == nil || s.state == nil {
		return nil
	}
	_, _, err := applyFamilyTransientBackoffSettingsUpdate(s.state.familyTransientBackoffPolicy(), items)
	return err
}

func (s *Scheduler) ApplySettingsUpdate(items []appsettings.ItemInput) {
	if s == nil || s.state == nil {
		return
	}
	policy, changed, err := applyFamilyTransientBackoffSettingsUpdate(s.state.familyTransientBackoffPolicy(), items)
	if err != nil || !changed {
		return
	}
	s.SetFamilyTransientBackoffPolicy(policy)
}

func (s *Scheduler) SetFamilyTransientBackoffPolicy(policy FamilyTransientBackoffPolicy) {
	if s == nil || s.state == nil {
		return
	}
	s.state.setFamilyTransientBackoffPolicy(policy)
}

func applyFamilyTransientBackoffSettingsUpdate(
	current FamilyTransientBackoffPolicy,
	items []appsettings.ItemInput,
) (FamilyTransientBackoffPolicy, bool, error) {
	next := current
	for _, item := range items {
		if item.Key != FamilyTransientBackoffSettingKey {
			continue
		}
		if item.Group != FamilyTransientBackoffSettingsGroup {
			return current, false, fmt.Errorf("%s 必须属于 %s 设置组", item.Key, FamilyTransientBackoffSettingsGroup)
		}
		seconds, err := parseFamilyTransientBackoffSeconds(item.Value)
		if err != nil {
			return current, false, err
		}
		next.Seconds = seconds
		return next, true, nil
	}
	return next, false, nil
}

func parseFamilyTransientBackoffSeconds(raw string) (int64, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("家族退避配置必须是整数秒")
	}
	if seconds < FamilyTransientBackoffExponential {
		return 0, fmt.Errorf("家族退避配置只能是 -1、0 或正整数")
	}
	if seconds > maxFamilyTransientBackoffSeconds {
		return 0, fmt.Errorf("家族退避时间过长")
	}
	return seconds, nil
}

func familyTransientBackoffSettingItem(value string) appsettings.ItemInput {
	return appsettings.ItemInput{
		Key:   FamilyTransientBackoffSettingKey,
		Value: value,
		Group: FamilyTransientBackoffSettingsGroup,
	}
}
