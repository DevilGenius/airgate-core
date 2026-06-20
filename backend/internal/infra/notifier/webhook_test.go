package notifier

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderBody(t *testing.T) {
	got := RenderBody(`{"text":"{{name}} owes {{amount}}","untouched":"{{missing}}"}`, map[string]string{
		"name":   "alice",
		"amount": "12",
	})
	want := `{"text":"alice owes 12","untouched":"{{missing}}"}`
	if got != want {
		t.Fatalf("RenderBody = %q, want %q", got, want)
	}
}

func TestSendWebhookValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  WebhookConfig
		want string
	}{
		{name: "blank url", cfg: WebhookConfig{Body: "{}"}, want: "webhook url is required"},
		{name: "invalid url", cfg: WebhookConfig{URL: "http://[::1", Body: "{}"}, want: "invalid webhook url"},
		{name: "bad scheme", cfg: WebhookConfig{URL: "ftp://example.com/hook", Body: "{}"}, want: "webhook url must use http or https"},
		{name: "blank body", cfg: WebhookConfig{URL: "https://example.com/hook", Body: " \t\n"}, want: "webhook request body is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SendWebhook(context.Background(), tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("SendWebhook() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestSendWebhookRequestCreateError(t *testing.T) {
	original := newWebhookRequest
	t.Cleanup(func() { newWebhookRequest = original })
	newWebhookRequest = func(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
		return nil, errors.New("boom")
	}

	err := SendWebhook(context.Background(), WebhookConfig{URL: "https://example.com/hook", Body: "{}"})
	if err == nil || !strings.Contains(err.Error(), "create webhook request: boom") {
		t.Fatalf("SendWebhook() error = %v", err)
	}
}

func TestSendWebhookSuccess(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "airgate-core-notifier" {
			t.Fatalf("User-Agent = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-AirGate-Webhook-Secret"); got != "secret-token" {
			t.Fatalf("X-AirGate-Webhook-Secret = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	err := SendWebhook(context.Background(), WebhookConfig{
		URL:    " " + server.URL + " ",
		Secret: " secret-token ",
		Body:   `{"ok":true}`,
	})
	if err != nil {
		t.Fatalf("SendWebhook() error = %v", err)
	}
	if gotBody != `{"ok":true}` {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestSendWebhookRejectedWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad hook", http.StatusBadGateway)
	}))
	defer server.Close()

	err := SendWebhook(context.Background(), WebhookConfig{URL: server.URL, Body: "{}"})
	if err == nil || !strings.Contains(err.Error(), "webhook returned HTTP 502: bad hook") {
		t.Fatalf("SendWebhook() error = %v", err)
	}
}

func TestSendWebhookRejectedWithoutBodyUsesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()

	err := SendWebhook(context.Background(), WebhookConfig{URL: server.URL, Body: "{}"})
	if err == nil || !strings.Contains(err.Error(), "webhook returned HTTP 504: 504 Gateway Timeout") {
		t.Fatalf("SendWebhook() error = %v", err)
	}
}

func TestSendWebhookContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := SendWebhook(ctx, WebhookConfig{URL: server.URL, Body: "{}"})
	if err == nil || !strings.Contains(err.Error(), "send webhook") || !errors.Is(err, context.Canceled) {
		t.Fatalf("SendWebhook() error = %v, want wrapped context.Canceled", err)
	}
}
