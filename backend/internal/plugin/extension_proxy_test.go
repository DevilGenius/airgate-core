package plugin

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/server/middleware"
)

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReadCloser) Close() error             { return nil }

func TestExtensionProxyBuildProxyRequest(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "https://core.example.test/api/v1/ext/demo/hello?debug=1", strings.NewReader("payload"))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Forwarded", "host=evil.example.test;proto=http")
	c.Request.Header.Set("X-Forwarded-Host", "evil.example.test")
	c.Request.Header.Set("X-Forwarded-Proto", "http")
	c.Request.Header.Set("X-Forwarded-For", "203.0.113.10")
	c.Request.Header.Set("X-Real-IP", "203.0.113.11")
	c.Request.Header.Set("X-Airgate-Entry", "admin")
	c.Request.Header.Set("X-Airgate-User-ID", "999")
	c.Request.Header.Set("X-Airgate-Role", "admin")
	c.Request.Header.Add("X-Custom", "a")
	c.Request.Header.Add("X-Custom", "b")
	c.Set(middleware.CtxKeyUserID, 123)
	c.Set(middleware.CtxKeyRole, "admin")

	req, err := (&ExtensionProxy{}).buildProxyRequest(c, "/hello", "user")
	if err != nil {
		t.Fatalf("buildProxyRequest() error = %v", err)
	}
	if req.Method != http.MethodPost || req.Path != "/hello" || req.Query != "debug=1" {
		t.Fatalf("request locator = %s %s?%s", req.Method, req.Path, req.Query)
	}
	if string(req.Body) != "payload" {
		t.Fatalf("body = %q, want payload", req.Body)
	}
	if got := req.Headers["content-type"].Values; len(got) != 1 || got[0] != "application/json" {
		t.Fatalf("content-type = %#v", got)
	}
	if got := req.Headers["x-custom"].Values; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("x-custom = %#v", got)
	}
	if got := req.Headers["x-airgate-entry"].Values[0]; got != "user" {
		t.Fatalf("x-airgate-entry = %q, want user", got)
	}
	if got := req.Headers["x-forwarded-host"].Values[0]; got != "core.example.test" {
		t.Fatalf("x-forwarded-host = %q, want core.example.test", got)
	}
	if got := req.Headers["x-forwarded-proto"].Values[0]; got != "https" {
		t.Fatalf("x-forwarded-proto = %q, want https", got)
	}
	for _, name := range []string{"forwarded", "x-forwarded-for", "x-real-ip"} {
		if _, ok := req.Headers[name]; ok {
			t.Fatalf("%s should not be forwarded: %#v", name, req.Headers[name])
		}
	}
	if got := req.Headers["x-airgate-user-id"].Values[0]; got != "123" {
		t.Fatalf("x-airgate-user-id = %q, want 123", got)
	}
	if got := req.Headers["x-airgate-role"].Values[0]; got != "admin" {
		t.Fatalf("x-airgate-role = %q, want admin", got)
	}
}

func TestExtensionProxyBuildProxyRequestDefaultsForwardedProto(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "http://core.example.test/api/v1/ext/demo", nil)

	req, err := (&ExtensionProxy{}).buildProxyRequest(c, "/", "admin")
	if err != nil {
		t.Fatalf("buildProxyRequest() error = %v", err)
	}
	if got := req.Headers["x-forwarded-proto"].Values[0]; got != "http" {
		t.Fatalf("x-forwarded-proto = %q, want http", got)
	}
}

func TestExtensionProxyBuildProxyRequestReadError(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "http://core.example.test/api/v1/ext/demo", nil)
	c.Request.Body = errReadCloser{}

	if _, err := (&ExtensionProxy{}).buildProxyRequest(c, "/", "admin"); err == nil {
		t.Fatal("buildProxyRequest() error = nil, want read error")
	}
}

func TestExtensionProxyHandleMissingPlugin(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	ep := NewExtensionProxy(&Manager{})
	router.Any("/api/v1/ext/:pluginName/*path", ep.Handle)
	router.Any("/api/v1/ext-user/:pluginName/*path", ep.Handle)
	router.Any("/api/v1/payment-callback/:pluginName/*path", ep.Handle)
	router.Any("/status", ep.HandleNamed("airgate-health", "public"))
	router.Any("/status/*path", ep.HandleNamed("airgate-health", "public"))

	for _, path := range []string{
		"/api/v1/ext/missing/foo",
		"/api/v1/ext-user/missing/foo",
		"/api/v1/payment-callback/missing/foo",
		"/status",
		"/status/ready",
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404; body=%s", path, recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), "extension") {
			t.Fatalf("%s body = %s", path, recorder.Body.String())
		}
	}
}

func TestIsStreamRequest(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("Accept", "application/json")
	if isStreamRequest(req) {
		t.Fatal("plain JSON request should not be treated as stream")
	}
	req.Header.Add("Accept", "text/event-stream; charset=utf-8")
	if !isStreamRequest(req) {
		t.Fatal("SSE accept header should be treated as stream")
	}
	if got, err := io.ReadAll(req.Body); err != nil || len(got) != 0 {
		t.Fatalf("request body unexpectedly changed: %q/%v", got, err)
	}
}
