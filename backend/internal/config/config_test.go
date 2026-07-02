package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetPortAndHostUseEnvironmentWithFallback(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("HOST", "")
	if got := GetPort(); got != DefaultPort {
		t.Fatalf("默认端口 = %d，期望 %d", got, DefaultPort)
	}
	if got := GetHost(); got != DefaultHost {
		t.Fatalf("默认主机 = %q，期望 %q", got, DefaultHost)
	}

	t.Setenv("PORT", "18080")
	t.Setenv("HOST", "127.0.0.1")
	if got := GetPort(); got != 18080 {
		t.Fatalf("环境端口 = %d，期望 18080", got)
	}
	if got := GetHost(); got != "127.0.0.1" {
		t.Fatalf("环境主机 = %q，期望 127.0.0.1", got)
	}

	t.Setenv("PORT", "bad")
	if got := GetPort(); got != DefaultPort {
		t.Fatalf("非法端口应回退默认值，得到 %d", got)
	}
}

func TestLoadAppliesDefaultsAndEnvironmentOverrides(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`
server:
  port: 9000
  mode: debug
  trusted_proxies:
    - 127.0.0.1
database:
  host: db.local
  port: 5432
  user: app
  password: secret
  dbname: airgate
redis:
  host: redis.local
  port: 6379
jwt:
  secret: yaml-secret
  expire_hour: 12
log:
  level: info
plugins:
  dir: data/plugins
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "18080")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.10")
	t.Setenv("DB_HOST", "db.env")
	t.Setenv("DB_PORT", "15432")
	t.Setenv("REDIS_TLS", "true")
	t.Setenv("REDIS_TLS_SERVER_NAME", "redis.service.local")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("PLUGINS_DIR", "env/plugins")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 18080 {
		t.Fatalf("服务器配置未被环境变量覆盖: %+v", cfg.Server)
	}
	if len(cfg.Server.TrustedProxies) != 2 || cfg.Server.TrustedProxies[0] != "10.0.0.0/8" || cfg.Server.TrustedProxies[1] != "192.168.1.10" {
		t.Fatalf("可信代理配置未被环境变量覆盖: %+v", cfg.Server.TrustedProxies)
	}
	if cfg.Database.Host != "db.env" || cfg.Database.Port != 15432 {
		t.Fatalf("数据库配置未被环境变量覆盖: %+v", cfg.Database)
	}
	if !cfg.Redis.TLS {
		t.Fatalf("Redis TLS 未被环境变量覆盖: %+v", cfg.Redis)
	}
	if cfg.Redis.TLSServerName != "redis.service.local" {
		t.Fatalf("Redis TLS ServerName 未被环境变量覆盖: %+v", cfg.Redis)
	}
	if cfg.Log.Level != "debug" || cfg.Plugins.Dir != "env/plugins" {
		t.Fatalf("日志或插件配置未被环境变量覆盖: log=%+v plugins=%+v", cfg.Log, cfg.Plugins)
	}
}

func TestLoadReturnsReadAndYAMLErrors(t *testing.T) {
	clearConfigEnv(t)
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("Load missing file returned nil error")
	}

	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
		t.Fatalf("写入非法配置失败: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load invalid YAML returned nil error")
	}
}

func TestLoadBackfillsEmptyHost(t *testing.T) {
	clearConfigEnv(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  host: \"\"\n"), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Server.Host != DefaultHost {
		t.Fatalf("Server.Host = %q, want %q", cfg.Server.Host, DefaultHost)
	}
}

func TestDatabaseDSNDefaultsSSLMode(t *testing.T) {
	dsn := DatabaseConfig{
		Host:     "db",
		Port:     5432,
		User:     "user",
		Password: "pass",
		DBName:   "airgate",
	}.DSN()

	want := "host=db port=5432 user=user password=pass dbname=airgate sslmode=disable"
	if dsn != want {
		t.Fatalf("DSN = %q，期望 %q", dsn, want)
	}
}

func TestAPIKeySecretValidatesConfiguredSecret(t *testing.T) {
	valid := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	cfg := &Config{Security: SecurityConfig{APIKeySecret: valid}}
	if got := cfg.APIKeySecret(); got != valid {
		t.Fatalf("有效密钥 = %q，期望使用配置值", got)
	}

	cfg.Security.APIKeySecret = "不是有效 hex"
	if got := cfg.APIKeySecret(); got == cfg.Security.APIKeySecret {
		t.Fatalf("非法密钥不应被采用")
	}
}

func TestValidateProductionRejectsWeakSecrets(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Mode: "release"},
		Database: DatabaseConfig{Password: "airgate"},
		JWT:      JWTConfig{Secret: "change-me-in-production"},
		Security: SecurityConfig{APIKeySecret: defaultAPIKeySecret},
	}
	if err := cfg.ValidateProduction(); err == nil {
		t.Fatal("ValidateProduction weak config error = nil")
	}

	cfg.Database.Password = "strong-db-password"
	cfg.JWT.Secret = "0123456789abcdef0123456789abcdef"
	cfg.Security.APIKeySecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("ValidateProduction strong config error = %v", err)
	}

	cfg.Server.Mode = "debug"
	cfg.JWT.Secret = ""
	cfg.Database.Password = ""
	cfg.Security.APIKeySecret = ""
	if err := cfg.ValidateProduction(); err != nil {
		t.Fatalf("debug mode should not enforce production secrets: %v", err)
	}
}

func TestConfigPathUsesEnvironment(t *testing.T) {
	t.Setenv("CONFIG_PATH", "")
	if got := ConfigPath(); got != "config.yaml" {
		t.Fatalf("默认配置路径 = %q，期望 config.yaml", got)
	}

	t.Setenv("CONFIG_PATH", "/tmp/airgate.yaml")
	if got := ConfigPath(); got != "/tmp/airgate.yaml" {
		t.Fatalf("配置路径 = %q，期望环境变量值", got)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"HOST", "PORT", "GIN_MODE",
		"TRUSTED_PROXIES",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE",
		"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB", "REDIS_TLS", "REDIS_TLS_SERVER_NAME",
		"JWT_SECRET", "JWT_EXPIRE_HOUR",
		"LOG_LEVEL", "LOG_FORMAT",
		"API_KEY_SECRET", "PLUGINS_DIR", "PLUGINS_MARKETPLACE_GITHUB_TOKEN",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}
