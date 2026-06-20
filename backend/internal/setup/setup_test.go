package setup

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

func TestEnvDBConfigRequiresCompleteEnvironment(t *testing.T) {
	clearSetupEnv(t)
	if got := EnvDBConfig(); got != nil {
		t.Fatalf("缺少环境变量时应返回 nil，得到 %+v", got)
	}

	t.Setenv("DB_HOST", "db")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "airgate")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "airgate")
	cfg := EnvDBConfig()
	if cfg == nil {
		t.Fatal("完整数据库环境变量应返回配置")
		return
	}
	if cfg.Host != "db" || cfg.Port != 5432 || cfg.User != "airgate" || cfg.SSLMode != "disable" {
		t.Fatalf("数据库配置异常: %+v", cfg)
	}

	t.Setenv("DB_SSLMODE", "require")
	if cfg := EnvDBConfig(); cfg == nil || cfg.SSLMode != "require" {
		t.Fatalf("DB_SSLMODE 未生效: %+v", cfg)
	}

	t.Setenv("DB_PORT", "bad")
	if got := EnvDBConfig(); got != nil {
		t.Fatalf("非法端口应返回 nil，得到 %+v", got)
	}
}

func TestEnvRedisConfigParsesOptionalDB(t *testing.T) {
	clearSetupEnv(t)
	if got := EnvRedisConfig(); got != nil {
		t.Fatalf("缺少环境变量时应返回 nil，得到 %+v", got)
	}

	t.Setenv("REDIS_HOST", "redis")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")
	cfg := EnvRedisConfig()
	if cfg == nil {
		t.Fatal("完整 Redis 环境变量应返回配置")
		return
	}
	if cfg.Host != "redis" || cfg.Port != 6379 || cfg.Password != "secret" || cfg.DB != 2 {
		t.Fatalf("Redis 配置异常: %+v", cfg)
	}

	t.Setenv("REDIS_DB", "bad")
	if cfg := EnvRedisConfig(); cfg == nil || cfg.DB != 0 {
		t.Fatalf("非法 Redis DB 应回退 0，得到 %+v", cfg)
	}

	t.Setenv("REDIS_PORT", "bad")
	if got := EnvRedisConfig(); got != nil {
		t.Fatalf("非法 Redis 端口应返回 nil，得到 %+v", got)
	}
}

func TestDatabaseNotExistDetection(t *testing.T) {
	if isDatabaseNotExistError(nil) {
		t.Fatal("nil 错误不应被识别为数据库不存在")
	}
	if !isDatabaseNotExistError(&pq.Error{Code: "3D000"}) {
		t.Fatal("PostgreSQL 3D000 应被识别为数据库不存在")
	}
	if !isDatabaseNotExistError(errors.New(`database "airgate" does not exist`)) {
		t.Fatal("包含 does not exist 的错误应被识别为数据库不存在")
	}
	if isDatabaseNotExistError(errors.New("permission denied")) {
		t.Fatal("权限错误不应被识别为数据库不存在")
	}
}

func TestSetupBootstrapErrorDetection(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "database missing", err: &pq.Error{Code: "3D000"}, want: true},
		{name: "users table missing", err: &pq.Error{Code: "42P01"}, want: true},
		{name: "users role missing", err: &pq.Error{Code: "42703"}, want: true},
		{name: "permission denied", err: &pq.Error{Code: "42501"}, want: false},
		{name: "network error", err: errors.New("connection refused"), want: false},
		{name: "nil", err: nil, want: false},
		{name: "string database missing", err: errors.New("database does not exist"), want: true},
		{name: "sqlite-style missing table", err: errors.New("no such table: users"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSetupBootstrapError(tt.err); got != tt.want {
				t.Fatalf("isSetupBootstrapError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQuoteIdentifierEscapesDoubleQuotes(t *testing.T) {
	got := quoteIdentifier(`air"gate`)
	want := `"air""gate"`
	if got != want {
		t.Fatalf("标识符 = %q，期望 %q", got, want)
	}
}

func TestNeedsSetupForMissingAndInvalidConfig(t *testing.T) {
	clearSetupEnv(t)
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yaml"))
	if !NeedsSetup() {
		t.Fatal("missing config should require setup")
	}

	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	t.Setenv("CONFIG_PATH", path)
	if !NeedsSetup() {
		t.Fatal("invalid config should require setup")
	}
}

func TestGenerateSecretReturnsHexSecret(t *testing.T) {
	secret := generateSecret()
	if len(secret) != 64 {
		t.Fatalf("secret length = %d, want 64", len(secret))
	}
	if _, err := hex.DecodeString(secret); err != nil {
		t.Fatalf("secret is not hex: %v", err)
	}
}

func TestSetupRoutesStatusAndBadJSON(t *testing.T) {
	clearSetupEnv(t)
	t.Setenv("CONFIG_PATH", filepath.Join(t.TempDir(), "missing.yaml"))
	router := gin.New()
	called := false
	RegisterRoutesWithCallback(router, func() { called = true })

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/setup/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", w.Code)
	}
	var statusResp struct {
		Code int `json:"code"`
		Data struct {
			NeedsSetup bool `json:"needs_setup"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !statusResp.Data.NeedsSetup {
		t.Fatalf("needs_setup = false, want true; body=%s", w.Body.String())
	}

	for _, path := range []string{"/setup/test-db", "/setup/test-redis", "/setup/install"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400; body=%s", path, w.Code, w.Body.String())
		}
	}
	if called {
		t.Fatal("install callback should not be called for bad JSON")
	}

	RegisterRoutes(gin.New())
}

func clearSetupEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE",
		"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB",
		"CONFIG_PATH",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
