package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	"github.com/gin-gonic/gin"
)

func TestNewServerRegistersCoreRoutesWithSQLite(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "server_new_server", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 0, Mode: "debug"},
		JWT:    config.JWTConfig{Secret: "test-secret", ExpireHour: 1},
		Log:    config.LogConfig{Level: "debug"},
		Plugins: config.PluginsConfig{
			Dir: t.TempDir(),
			Marketplace: config.MarketplaceConfig{
				Disabled: true,
			},
		},
	}

	s, err := NewServer(cfg, db, nil)
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}
	if s == nil || s.engine == nil || s.handlers == nil || s.srv == nil {
		t.Fatalf("NewServer returned incomplete server: %#v", s)
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("healthz response = %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/missing/frontend/path", nil)
	w = httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SPA fallback status = %d body=%q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("SPA fallback content-type = %q", got)
	}
}

func TestServerStartReturnsListenError(t *testing.T) {
	s := &Server{
		cfg: &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: 0}},
		srv: &http.Server{Addr: "127.0.0.1:-1", Handler: http.NewServeMux()},
	}

	if err := s.Start(); err == nil {
		t.Fatal("Start returned nil error for invalid listen address")
	}
}

func TestConfigureTrustedProxiesRejectsInvalidConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	if err := configureTrustedProxies(engine, nil); err != nil {
		t.Fatalf("empty trusted proxies should be valid: %v", err)
	}

	engine = gin.New()
	if err := configureTrustedProxies(engine, []string{"127.0.0.1", "10.0.0.0/8"}); err != nil {
		t.Fatalf("valid trusted proxies returned error: %v", err)
	}

	engine = gin.New()
	if err := configureTrustedProxies(engine, []string{"not-a-cidr"}); err == nil {
		t.Fatal("invalid trusted proxy config returned nil error")
	}
}

func TestServerShutdownStopsStartedComponents(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "server_shutdown", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	recorder := billing.NewRecorder(db, 1)
	recorder.Start()

	s := &Server{
		cfg:         &config.Config{},
		srv:         &http.Server{},
		recorder:    recorder,
		marketplace: plugin.NewMarketplace(t.TempDir()),
		pluginMgr:   plugin.NewManager(t.TempDir(), "", "", db),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
}

func TestServerStartPluginsInitializesBackgroundComponents(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "server_start_plugins", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 0, Mode: "debug"},
		JWT:    config.JWTConfig{Secret: "test-secret", ExpireHour: 1},
		Log:    config.LogConfig{Level: "debug"},
		Plugins: config.PluginsConfig{
			Dir: t.TempDir(),
			Marketplace: config.MarketplaceConfig{
				Disabled: true,
			},
		},
	}
	s, err := NewServer(cfg, db, nil)
	if err != nil {
		t.Fatalf("NewServer returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.StartPlugins(ctx)
	if s.pluginStartCancel == nil {
		t.Fatal("StartPlugins did not install cancel function")
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown after StartPlugins returned error: %v", err)
	}
}
