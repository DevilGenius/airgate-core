// Package notification provides core message notification use cases.
package notification

import (
	"context"
	"fmt"
	"strings"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	"github.com/DevilGenius/airgate-core/internal/infra/notifier"
)

const (
	GroupName = "notification"

	KeyWebhookURL    = "notification_webhook_url"
	KeyWebhookSecret = "notification_webhook_secret"
	KeyWebhookBody   = "notification_webhook_body"
)

// Service sends configured core message notifications.
type Service struct {
	settingsService *appsettings.Service
}

// NewService creates a message notification service.
func NewService(settingsService *appsettings.Service) *Service {
	return &Service{settingsService: settingsService}
}

// Send renders the saved notification body template and posts it to the saved webhook.
func (s *Service) Send(ctx context.Context, values map[string]string) error {
	cfg, err := s.LoadConfig(ctx)
	if err != nil {
		return err
	}
	return s.SendWithConfig(ctx, cfg, values)
}

// IsConfigured reports whether saved webhook notification settings are usable.
func (s *Service) IsConfigured(ctx context.Context) (bool, error) {
	if s == nil || s.settingsService == nil {
		return false, nil
	}
	items, err := s.settingsService.List(ctx, GroupName)
	if err != nil {
		return false, fmt.Errorf("load notification settings: %w", err)
	}
	hasURL := false
	hasBody := false
	for _, item := range items {
		switch item.Key {
		case KeyWebhookURL:
			hasURL = strings.TrimSpace(item.Value) != ""
		case KeyWebhookBody:
			hasBody = strings.TrimSpace(item.Value) != ""
		}
	}
	return hasURL && hasBody, nil
}

// Test renders and sends a notification using unsaved form values from the settings page.
func (s *Service) Test(ctx context.Context, webhookURL, secret, body string) error {
	return s.SendWithConfig(ctx, notifier.WebhookConfig{
		URL:    webhookURL,
		Secret: secret,
		Body:   body,
	}, s.DefaultTemplateValues("测试标题", "测试内容"))
}

// SendWithConfig renders cfg.Body and posts it to cfg.URL.
func (s *Service) SendWithConfig(ctx context.Context, cfg notifier.WebhookConfig, values map[string]string) error {
	cfg.Body = notifier.RenderBody(cfg.Body, values)
	return notifier.SendWebhook(ctx, cfg)
}

// LoadConfig reads the saved webhook notification settings.
func (s *Service) LoadConfig(ctx context.Context) (notifier.WebhookConfig, error) {
	if s == nil || s.settingsService == nil {
		return notifier.WebhookConfig{}, fmt.Errorf("notification settings service is not configured")
	}

	items, err := s.settingsService.List(ctx, GroupName)
	if err != nil {
		return notifier.WebhookConfig{}, fmt.Errorf("load notification settings: %w", err)
	}

	cfg := notifier.WebhookConfig{}
	for _, item := range items {
		switch item.Key {
		case KeyWebhookURL:
			cfg.URL = item.Value
		case KeyWebhookSecret:
			cfg.Secret = item.Value
		case KeyWebhookBody:
			cfg.Body = item.Value
		}
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return notifier.WebhookConfig{}, fmt.Errorf("notification webhook url is not configured")
	}
	if strings.TrimSpace(cfg.Body) == "" {
		return notifier.WebhookConfig{}, fmt.Errorf("notification webhook body is not configured")
	}
	return cfg, nil
}

// DefaultTemplateValues returns common template variables for notification bodies.
func (s *Service) DefaultTemplateValues(title, content string) map[string]string {
	return map[string]string{
		"title":   title,
		"content": content,
	}
}
