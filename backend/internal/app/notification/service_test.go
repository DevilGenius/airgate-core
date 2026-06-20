package notification

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	"github.com/DevilGenius/airgate-core/internal/infra/notifier"
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

func TestNilServiceAndSettingsErrors(t *testing.T) {
	if got, err := (*Service)(nil).IsConfigured(t.Context()); err != nil || got {
		t.Fatalf("nil IsConfigured() = %v/%v", got, err)
	}
	if _, err := (*Service)(nil).LoadConfig(t.Context()); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("nil LoadConfig() error = %v", err)
	}

	repoErr := errors.New("settings failed")
	service := NewService(appsettings.NewService(notificationSettingsRepo{err: repoErr}))
	if _, err := service.IsConfigured(t.Context()); !errors.Is(err, repoErr) {
		t.Fatalf("IsConfigured settings error = %v", err)
	}
	if _, err := service.LoadConfig(t.Context()); !errors.Is(err, repoErr) {
		t.Fatalf("LoadConfig settings error = %v", err)
	}
}

func TestLoadConfigRequiresURLAndBody(t *testing.T) {
	_, err := newTestService([]appsettings.Setting{
		{Key: KeyEnabled, Value: "true", Group: GroupName},
		{Key: KeyWebhookBody, Value: `{"title":"{{title}}"}`, Group: GroupName},
	}).LoadConfig(t.Context())
	if err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("missing URL error = %v", err)
	}

	_, err = newTestService([]appsettings.Setting{
		{Key: KeyEnabled, Value: "true", Group: GroupName},
		{Key: KeyWebhookURL, Value: "https://example.com/webhook", Group: GroupName},
	}).LoadConfig(t.Context())
	if err == nil || !strings.Contains(err.Error(), "body") {
		t.Fatalf("missing body error = %v", err)
	}
}

func TestSendAndTestPostRenderedWebhook(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		bodies = append(bodies, string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service := newTestService([]appsettings.Setting{
		{Key: KeyEnabled, Value: "true", Group: GroupName},
		{Key: KeyWebhookURL, Value: server.URL, Group: GroupName},
		{Key: KeyWebhookBody, Value: `{"title":"{{title}}","content":"{{content}}"}`, Group: GroupName},
	})
	if err := service.Send(t.Context(), map[string]string{"title": "Hello", "content": "World"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := service.Test(t.Context(), server.URL, "", `{"title":"{{title}}","content":"{{content}}"}`); err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], "Hello") || !strings.Contains(bodies[1], "测试标题") {
		t.Fatalf("webhook bodies = %#v", bodies)
	}
}

func TestSendPropagatesLoadConfigError(t *testing.T) {
	service := newTestService([]appsettings.Setting{
		{Key: KeyEnabled, Value: "false", Group: GroupName},
	})
	if err := service.Send(t.Context(), map[string]string{"title": "Hello"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Send() error = %v, want disabled load config error", err)
	}
}

func TestSendWithConfigPropagatesWebhookError(t *testing.T) {
	service := NewService(nil)
	err := service.SendWithConfig(t.Context(), notifier.WebhookConfig{
		URL:  "://bad-url",
		Body: `{"title":"{{title}}"}`,
	}, map[string]string{"title": "Hello"})
	if err == nil {
		t.Fatal("SendWithConfig() error = nil, want invalid URL error")
	}
}

func TestDefaultTemplateValues(t *testing.T) {
	values := NewService(nil).DefaultTemplateValues("Title", "Content")
	if values["title"] != "Title" || values["content"] != "Content" {
		t.Fatalf("DefaultTemplateValues() = %#v", values)
	}
}

func newTestService(items []appsettings.Setting) *Service {
	return NewService(appsettings.NewService(notificationSettingsRepo{items: items}))
}

type notificationSettingsRepo struct {
	items []appsettings.Setting
	err   error
}

func (r notificationSettingsRepo) List(context.Context, string) ([]appsettings.Setting, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]appsettings.Setting{}, r.items...), nil
}

func (r notificationSettingsRepo) UpsertMany(context.Context, []appsettings.ItemInput) error {
	return nil
}
