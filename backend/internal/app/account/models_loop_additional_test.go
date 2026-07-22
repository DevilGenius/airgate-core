package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartTokenRefreshLoopRunsInitialRefresh(t *testing.T) {
	called := make(chan struct{})
	service := NewService(stubRepository{
		listAll: func(context.Context, ListFilter) ([]Account, error) {
			close(called)
			return nil, errors.New("stop after initial list")
		},
	}, stubPluginCatalog{}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.StartTokenRefreshLoop(ctx)

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("token refresh loop did not run initial refresh")
	}
}

func TestAPIKeyUpstreamModelsHTTPBranches(t *testing.T) {
	var sawAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			t.Fatalf("models path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"data":[
				{"id":" gpt-4.1 ","name":" GPT 4.1 "},
				{"id":"gpt-4.1","name":"duplicate"},
				{"id":"gpt-4.1-mini"},
				{"id":" "}
			]
		}`))
	}))
	defer server.Close()

	models, err := getAPIKeyUpstreamModels(context.Background(), Account{
		Type:        "apikey",
		Credentials: map[string]string{"api_key": "sk-test", "base_url": server.URL},
	})
	if err != nil {
		t.Fatalf("getAPIKeyUpstreamModels success error = %v", err)
	}
	if sawAuthorization != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", sawAuthorization)
	}
	if len(models) != 2 || models[0] != (Model{ID: "gpt-4.1", Name: "GPT 4.1"}) ||
		models[1] != (Model{ID: "gpt-4.1-mini", Name: "gpt-4.1-mini"}) {
		t.Fatalf("models = %+v", models)
	}

	if _, err := getAPIKeyUpstreamModels(context.Background(), Account{Credentials: map[string]string{"base_url": server.URL}}); err == nil {
		t.Fatal("missing api_key should fail")
	}

	rejectingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusTeapot)
	}))
	defer rejectingServer.Close()
	if _, err := getAPIKeyUpstreamModels(context.Background(), Account{
		Credentials: map[string]string{"api_key": "sk-test", "base_url": rejectingServer.URL},
	}); err == nil || !strings.Contains(err.Error(), "HTTP 418") {
		t.Fatalf("HTTP error = %v", err)
	}

	if got := buildAPIKeyModelsURL(" https://example.test/v1/ "); got != "https://example.test/v1/models" {
		t.Fatalf("buildAPIKeyModelsURL /v1 = %q", got)
	}
	if got := buildAPIKeyModelsURL(""); got != "https://api.openai.com/v1/models" {
		t.Fatalf("buildAPIKeyModelsURL default = %q", got)
	}

	if _, err := accountHTTPClient(&Proxy{Protocol: "http", Address: "[", Port: 8080}); err == nil {
		t.Fatal("invalid proxy URL should fail")
	}
}
