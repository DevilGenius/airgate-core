package notification

import (
	"context"
	"strings"
	"testing"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
)

func TestIsConfiguredRequiresExplicitEnable(t *testing.T) {
	base := []appsettings.Setting{
		{Key: KeyWebhookURL, Value: "https://example.com/webhook", Group: GroupName},
		{Key: KeyWebhookBody, Value: `{"title":"{{title}}"}`, Group: GroupName},
	}

	tests := []struct {
		name string
		item appsettings.Setting
		want bool
	}{
		{name: "missing enabled defaults disabled", want: false},
		{name: "enabled true", item: appsettings.Setting{Key: KeyEnabled, Value: "true", Group: GroupName}, want: true},
		{name: "enabled true ignores case and spaces", item: appsettings.Setting{Key: KeyEnabled, Value: " TRUE ", Group: GroupName}, want: true},
		{name: "enabled false", item: appsettings.Setting{Key: KeyEnabled, Value: "false", Group: GroupName}, want: false},
		{name: "enabled empty", item: appsettings.Setting{Key: KeyEnabled, Value: "", Group: GroupName}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := append([]appsettings.Setting{}, base...)
			if tt.item.Key != "" {
				items = append(items, tt.item)
			}
			got, err := newTestService(items).IsConfigured(t.Context())
			if err != nil {
				t.Fatalf("IsConfigured() returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfigReturnsDisabledWhenNotExplicitlyEnabled(t *testing.T) {
	items := []appsettings.Setting{
		{Key: KeyWebhookURL, Value: "https://example.com/webhook", Group: GroupName},
		{Key: KeyWebhookBody, Value: `{"title":"{{title}}"}`, Group: GroupName},
	}

	_, err := newTestService(items).LoadConfig(t.Context())
	if err == nil || !strings.Contains(err.Error(), "notification is disabled") {
		t.Fatalf("LoadConfig() error = %v, want disabled error", err)
	}
}

func TestLoadConfigReadsEnabledWebhookSettings(t *testing.T) {
	items := []appsettings.Setting{
		{Key: KeyEnabled, Value: "true", Group: GroupName},
		{Key: KeyWebhookURL, Value: "https://example.com/webhook", Group: GroupName},
		{Key: KeyWebhookSecret, Value: "secret", Group: GroupName},
		{Key: KeyWebhookBody, Value: `{"title":"{{title}}"}`, Group: GroupName},
	}

	cfg, err := newTestService(items).LoadConfig(t.Context())
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}
	if cfg.URL != "https://example.com/webhook" || cfg.Secret != "secret" || cfg.Body == "" {
		t.Fatalf("LoadConfig() = %+v, want saved webhook config", cfg)
	}
}

func newTestService(items []appsettings.Setting) *Service {
	return NewService(appsettings.NewService(notificationSettingsRepo{items: items}))
}

type notificationSettingsRepo struct {
	items []appsettings.Setting
}

func (r notificationSettingsRepo) List(context.Context, string) ([]appsettings.Setting, error) {
	return append([]appsettings.Setting{}, r.items...), nil
}

func (r notificationSettingsRepo) UpsertMany(context.Context, []appsettings.ItemInput) error {
	return nil
}
