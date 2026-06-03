// Package notifier provides outbound message notification senders.
package notifier

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultWebhookTimeout = 10 * time.Second
	maxErrorBodyBytes     = 4096
)

// WebhookConfig describes a single webhook notification request.
type WebhookConfig struct {
	URL    string
	Secret string
	Body   string
}

// RenderBody replaces {{name}} placeholders with their corresponding values.
func RenderBody(template string, values map[string]string) string {
	body := template
	for key, value := range values {
		body = strings.ReplaceAll(body, "{{"+key+"}}", value)
	}
	return body
}

// SendWebhook posts the configured body to the webhook URL.
func SendWebhook(ctx context.Context, cfg WebhookConfig) error {
	endpoint := strings.TrimSpace(cfg.URL)
	if endpoint == "" {
		return fmt.Errorf("webhook url is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid webhook url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("webhook url must use http or https")
	}
	if strings.TrimSpace(cfg.Body) == "" {
		return fmt.Errorf("webhook request body is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(cfg.Body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "airgate-core-notifier")
	if secret := strings.TrimSpace(cfg.Secret); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("X-AirGate-Webhook-Secret", secret)
	}

	client := &http.Client{Timeout: defaultWebhookTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		slog.Warn("notification_webhook_rejected", "status", resp.StatusCode, "url", endpoint)
		return fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, msg)
	}

	slog.Info("notification_webhook_sent", "status", resp.StatusCode, "url", endpoint)
	return nil
}
