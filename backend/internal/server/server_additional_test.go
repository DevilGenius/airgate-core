package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func newServerTestContext(method, target string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = params
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

func TestExtractCCBearerKeyAndMissingBalanceAuth(t *testing.T) {
	c, _ := newServerTestContext(http.MethodGet, "/v1/usage", nil)
	if got := extractCCBearerKey(c); got != "" {
		t.Fatalf("missing bearer = %q", got)
	}
	c.Request.Header.Set("Authorization", "Basic sk-test")
	if got := extractCCBearerKey(c); got != "" {
		t.Fatalf("basic bearer = %q", got)
	}
	c.Request.Header.Set("Authorization", "Bearer  sk-test  ")
	if got := extractCCBearerKey(c); got != "sk-test" {
		t.Fatalf("bearer = %q", got)
	}

	s := &Server{}
	for _, header := range []string{"", "Bearer bad"} {
		c, w := newServerTestContext(http.MethodGet, "/v1/usage", nil)
		if header != "" {
			c.Request.Header.Set("Authorization", header)
		}
		s.handleCCCompatUserBalance(c)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"is_active":false`) {
			t.Fatalf("header %q status/body = %d %s", header, w.Code, w.Body.String())
		}
	}
}

func TestCCCompatUserBalanceWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "server_cc_compat", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	user, err := db.User.Create().
		SetEmail("cc@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	s := &Server{db: db}

	cases := []struct {
		name       string
		key        string
		quota      float64
		used       float64
		expiresAt  *time.Time
		wantActive bool
		wantBody   string
	}{
		{name: "limited", key: "sk-limited", quota: 10, used: 4, wantActive: true, wantBody: `"balance":6`},
		{name: "overused", key: "sk-overused", quota: 3, used: 5, wantActive: false, wantBody: `"balance":0`},
		{name: "unlimited", key: "sk-unlimited", quota: 0, used: 99, wantActive: true, wantBody: `"balance":1000000`},
		{name: "expired", key: "sk-expired", quota: 10, used: 1, expiresAt: timePtr(time.Now().Add(-time.Hour)), wantActive: false, wantBody: "api key expired"},
	}
	for _, tt := range cases {
		if _, err := db.APIKey.Create().
			SetName(tt.name).
			SetKeyHash(auth.HashAPIKey(tt.key)).
			SetUser(user).
			SetQuotaUsd(tt.quota).
			SetUsedQuota(tt.used).
			SetNillableExpiresAt(tt.expiresAt).
			Save(ctx); err != nil {
			t.Fatalf("create api key %s: %v", tt.name, err)
		}
		c, w := newServerTestContext(http.MethodGet, "/v1/usage", nil)
		c.Request.Header.Set("Authorization", "Bearer "+tt.key)
		s.handleCCCompatUserBalance(c)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", tt.name, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), tt.wantBody) {
			t.Fatalf("%s body = %s, want %q", tt.name, w.Body.String(), tt.wantBody)
		}
		if strings.Contains(w.Body.String(), `"is_active":true`) != tt.wantActive {
			t.Fatalf("%s active body = %s", tt.name, w.Body.String())
		}
	}

	c, w := newServerTestContext(http.MethodGet, "/v1/usage", nil)
	c.Request.Header.Set("Authorization", "Bearer sk-missing")
	s.handleCCCompatUserBalance(c)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid api key") {
		t.Fatalf("missing key status/body = %d %s", w.Code, w.Body.String())
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestDynamicRouterRemoveRoutesAndAllowedMethods(t *testing.T) {
	dr := NewDynamicRouter(&plugin.Forwarder{})
	routes := []routeEntry{
		{Method: http.MethodGet, Path: "/v1/test"},
		{Method: http.MethodPost, Path: "/v1/test"},
	}
	dr.AddRoutes("test", routes)
	dr.RemoveRoutes("test", []routeEntry{{Method: http.MethodGet, Path: "/v1/test"}})

	dr.mu.RLock()
	methods := allowedMethodsForPathLocked(dr.routes, "/v1/test")
	dr.mu.RUnlock()
	if len(methods) != 1 || methods[0] != http.MethodPost {
		t.Fatalf("allowed methods after remove = %#v", methods)
	}

	c, w := newServerTestContext(http.MethodGet, "/v1/unknown", nil)
	dr.Handle(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown path status = %d", w.Code)
	}

	dr.RemoveRoutes("test", []routeEntry{{Method: http.MethodPost, Path: "/v1/test"}})
	dr.mu.RLock()
	remaining := len(dr.routes)
	dr.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("routes after remove = %d", remaining)
	}

	broken := map[string]bool{
		"BROKEN":              true,
		"GET /v1/test":        false,
		"POST /v1/other":      true,
		"DELETE /v1/expected": true,
	}
	if got := allowedMethodsForPathLocked(broken, "/v1/expected"); len(got) != 1 || got[0] != http.MethodDelete {
		t.Fatalf("allowed methods with broken entries = %#v", got)
	}
}

func TestDynamicRouterHandleAdditionalBranches(t *testing.T) {
	nilRouter := NewDynamicRouter(nil)
	c, w := newServerTestContext(http.MethodGet, "/v1/test", nil)
	nilRouter.Handle(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil forwarder status = %d body=%s", w.Code, w.Body.String())
	}

	dr := NewDynamicRouter(&plugin.Forwarder{})
	dr.AddRoutes("test", []routeEntry{{Method: http.MethodGet, Path: "/v1/test"}})
	c, w = newServerTestContext(http.MethodPost, "/v1/test", gin.Params{{Key: "path", Value: "/v1/test"}})
	dr.Handle(c)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method mismatch response = %d allow=%q body=%s", w.Code, w.Header().Get("Allow"), w.Body.String())
	}

	c, w = newServerTestContext(http.MethodGet, "/v1/test", gin.Params{{Key: "path", Value: "/v1/test"}})
	dr.Handle(c)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "missing_api_key") {
		t.Fatalf("matched route should enter forwarder auth path, got %d %s", w.Code, w.Body.String())
	}

	openRouter := NewDynamicRouter(&plugin.Forwarder{})
	c, w = newServerTestContext(http.MethodGet, "/v1/anything", nil)
	openRouter.Handle(c)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "missing_api_key") {
		t.Fatalf("open router should enter forwarder auth path, got %d %s", w.Code, w.Body.String())
	}
}

func TestServePluginAssetFallbacks(t *testing.T) {
	baseDir := t.TempDir()
	assetDir := filepath.Join(baseDir, "demo", "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}
	mgr := plugin.NewManager(baseDir, "", "", nil)
	handler := servePluginAsset(mgr, baseDir)

	c, w := newServerTestContext(http.MethodGet, "/plugins/demo/assets/app.js", gin.Params{
		{Key: "name", Value: "demo"},
		{Key: "path", Value: "/app.js"},
	})
	handler(c)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/javascript; charset=utf-8" ||
		!strings.Contains(w.Body.String(), "console.log") {
		t.Fatalf("app.js response = %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}

	c, w = newServerTestContext(http.MethodGet, "/plugins/demo/assets/index.css", gin.Params{
		{Key: "name", Value: "demo"},
		{Key: "path", Value: "/index.css"},
	})
	handler(c)
	if w.Code != http.StatusOK {
		t.Fatalf("missing index.css response = %d", w.Code)
	}

	c, w = newServerTestContext(http.MethodGet, "/plugins/demo/assets/missing.js", gin.Params{
		{Key: "name", Value: "demo"},
		{Key: "path", Value: "/missing.js"},
	})
	handler(c)
	if status := c.Writer.Status(); status != http.StatusNotFound {
		t.Fatalf("missing asset status = %d recorder=%d", status, w.Code)
	}

	c, w = newServerTestContext(http.MethodGet, "/plugins/demo/assets/bad", gin.Params{
		{Key: "name", Value: "demo"},
		{Key: "path", Value: `..secret.js`},
	})
	handler(c)
	if status := c.Writer.Status(); status != http.StatusBadRequest {
		t.Fatalf("traversal status = %d recorder=%d", status, w.Code)
	}
}

func TestHandleRuntimeAssetRejectsInvalidPathAndContentTypes(t *testing.T) {
	s := &Server{}
	for _, rawPath := range []string{"", "."} {
		c, w := newServerTestContext(http.MethodGet, "/assets-runtime/"+rawPath, gin.Params{{Key: "path", Value: rawPath}})
		s.handleRuntimeAsset(c)
		if status := c.Writer.Status(); status != http.StatusBadRequest {
			t.Fatalf("runtime path %q status = %d recorder=%d", rawPath, status, w.Code)
		}
	}

	for _, tt := range []struct {
		name string
		want string
	}{
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.webp", "image/webp"},
		{"anim.gif", "image/gif"},
		{"movie.mp4", "video/mp4"},
		{"sound.mp3", "audio/mpeg"},
	} {
		if got := contentTypeFromExt(tt.name); got != tt.want {
			t.Fatalf("contentTypeFromExt(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestHandleRuntimeAssetStorageInitError(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "server_runtime_asset_closed_db", schema.WithGlobalUniqueID(false))
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	s := &Server{db: db}
	c, _ := newServerTestContext(http.MethodGet, "/assets-runtime/chat/1/a.png", gin.Params{{Key: "path", Value: "/chat/1/a.png"}})
	s.handleRuntimeAsset(c)
	if status := c.Writer.Status(); status != http.StatusInternalServerError {
		t.Fatalf("closed db runtime asset status = %d", status)
	}
}
