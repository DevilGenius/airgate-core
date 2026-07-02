// Package config 提供配置管理（YAML 文件 + 环境变量覆盖）
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPort 默认服务端口
const DefaultPort = 9517

// DefaultHost 默认监听地址
//
// 127.0.0.1 = 仅本机可访问（需要通过反向代理或防火墙转发才能暴露）
// 0.0.0.0 = 绑定所有接口，局域网/公网可达（取决于防火墙），必须显式配置
const DefaultHost = "127.0.0.1"

// GetPort 获取服务端口（优先环境变量 PORT）
func GetPort() int {
	if v := os.Getenv("PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			return p
		}
	}
	return DefaultPort
}

// GetHost 获取监听地址（优先环境变量 HOST）
//
// 仅用于安装向导阶段（还没有 config.yaml 的时候），主服务启动后以 cfg.Server.Host 为准。
func GetHost() string {
	if v := os.Getenv("HOST"); v != "" {
		return v
	}
	return DefaultHost
}

// Config 应用配置
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	JWT      JWTConfig      `yaml:"jwt"`
	Security SecurityConfig `yaml:"security"`
	Log      LogConfig      `yaml:"log"`
	Plugins  PluginsConfig  `yaml:"plugins"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `yaml:"level"`  // debug/info/warn/error，默认 info
	Format string `yaml:"format"` // text/json，默认 text
}

// PluginsConfig 插件配置
type PluginsConfig struct {
	Dir         string            `yaml:"dir"`         // 插件二进制目录，默认 data/plugins
	Dev         []DevPlugin       `yaml:"dev"`         // 开发模式：直接从源码加载的插件
	Marketplace MarketplaceConfig `yaml:"marketplace"` // 插件市场配置
}

// MarketplaceConfig 插件市场配置
type MarketplaceConfig struct {
	Disabled        bool          `yaml:"disabled"`         // 关闭市场后台同步（默认开启）
	RefreshInterval time.Duration `yaml:"refresh_interval"` // 同步间隔，默认 1h
	GithubToken     string        `yaml:"github_token"`     // GitHub Token，提高 API 限流上限
	Plugins         []MarketEntry `yaml:"plugins"`          // 自定义市场条目（可选，覆盖默认列表）
}

// MarketEntry 市场插件条目（绑定到 GitHub 仓库）
type MarketEntry struct {
	Name        string `yaml:"name"`        // 插件名称（与 Info().ID 一致）
	Description string `yaml:"description"` // 描述（兜底值，未拉到 release 时使用）
	Author      string `yaml:"author"`      // 作者
	Type        string `yaml:"type"`        // gateway / payment / extension
	GithubRepo  string `yaml:"github_repo"` // owner/repo
}

// DevPlugin 开发模式插件
type DevPlugin struct {
	Name string `yaml:"name"` // 插件名称提示值（兼容字段，实际以插件 Info().ID 为准）
	Path string `yaml:"path"` // 源码目录路径（用 go run 启动）
}

// ServerConfig HTTP 服务器配置
//
// 注：早期版本曾有 WebDir 字段（默认 "web/dist"）。从 2026 起前端 SPA 已通过
// //go:embed 打进二进制（见 internal/web 包），不再需要单独的静态目录配置。
// 如果旧的 config.yaml 里仍有 web_dir，会被 yaml 解析器静默忽略，无需手工清理。
type ServerConfig struct {
	// Host 监听地址：
	//   - "127.0.0.1"（默认）仅本机访问
	//   - "0.0.0.0" 绑定所有网卡，局域网可达
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"` // debug / release
	// TrustedProxies 显式可信反向代理 IP/CIDR。为空表示不信任任何代理头。
	TrustedProxies []string `yaml:"trusted_proxies"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
	// MaxOpenConns 连接池最大连接数。默认 50。
	// 必须小于 Postgres 的 max_connections，否则高并发会打出 "too many clients"。
	MaxOpenConns int `yaml:"max_open_conns"`
	// MaxIdleConns 最大空闲连接数。默认 25。
	MaxIdleConns int `yaml:"max_idle_conns"`
	// ConnMaxLifetimeMinutes 连接最大生命周期（分钟）。默认 30。
	ConnMaxLifetimeMinutes int `yaml:"conn_max_lifetime_minutes"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	Password      string `yaml:"password"`
	DB            int    `yaml:"db"`
	TLS           bool   `yaml:"tls"`
	TLSServerName string `yaml:"tls_server_name"`
}

// JWTConfig JWT 配置
type JWTConfig struct {
	Secret     string `yaml:"secret"`
	ExpireHour int    `yaml:"expire_hour"`
}

// defaultAPIKeySecret 内置默认 API Key 加密密钥（hex 编码 32 字节）
// 用户未配置或配置格式不合规时自动使用此值
const defaultAPIKeySecret = "6a8f3d2e1b9c4f7a0e5d2c8b3a1f6e9d4c7b2a5e8f1d3c6b9a2e5f8d1c4b7a0e"

// APIKeySecret 返回实际使用的 API Key 加密密钥：
// 优先使用配置值（需为合法 32 字节 hex），否则使用内置默认值。
// 生产模式必须先通过 ValidateProduction，禁止落到内置默认值。
func (c *Config) APIKeySecret() string {
	s := strings.TrimSpace(c.Security.APIKeySecret)
	if validAPIKeySecret(s) {
		return s
	}
	return defaultAPIKeySecret
}

func validAPIKeySecret(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}

// SecurityConfig 安全相关配置
type SecurityConfig struct {
	APIKeySecret string `yaml:"api_key_secret"` // API Key 加密密钥（32 字节 hex，即 64 字符）
}

// DSN 返回 PostgreSQL 连接字符串
func (d DatabaseConfig) DSN() string {
	sslmode := d.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	return "host=" + d.Host +
		" port=" + strconv.Itoa(d.Port) +
		" user=" + d.User +
		" password=" + d.Password +
		" dbname=" + d.DBName +
		" sslmode=" + sslmode
}

// Load 从 YAML 文件加载配置，环境变量优先级高于配置文件
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Server: ServerConfig{Host: DefaultHost, Port: DefaultPort, Mode: "release"},
		JWT:    JWTConfig{ExpireHour: 24},
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	// 旧 config.yaml 没有 host 字段时，YAML unmarshal 会把 Host 置空，这里补回默认值
	if cfg.Server.Host == "" {
		cfg.Server.Host = DefaultHost
	}
	applyEnvOverrides(cfg)
	return cfg, nil
}

func (c *Config) ValidateProduction() error {
	if !strings.EqualFold(strings.TrimSpace(c.Server.Mode), "release") {
		return nil
	}

	var problems []string
	if weakPlainSecret(c.JWT.Secret, "change-me-in-production", "airgate-docker-secret-change-me", "airgate-default-secret-change-me", "change-me-to-a-long-random-string") || len(strings.TrimSpace(c.JWT.Secret)) < 32 {
		problems = append(problems, "jwt.secret must be a non-default secret with at least 32 characters")
	}
	if weakPlainSecret(c.Database.Password, "airgate", "postgres", "change-me-please") {
		problems = append(problems, "database.password must be non-empty and not a public default")
	}
	apiKeySecret := strings.TrimSpace(c.Security.APIKeySecret)
	if !validAPIKeySecret(apiKeySecret) || apiKeySecret == defaultAPIKeySecret || weakPlainSecret(apiKeySecret, "change-me-please") {
		problems = append(problems, "security.api_key_secret must be a unique 32-byte hex secret")
	}
	if len(problems) > 0 {
		return fmt.Errorf("production configuration is insecure: %s", strings.Join(problems, "; "))
	}
	return nil
}

func weakPlainSecret(value string, weakValues ...string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	for _, weak := range weakValues {
		if strings.EqualFold(value, weak) {
			return true
		}
	}
	return false
}

// applyEnvOverrides 用环境变量覆盖配置值
func applyEnvOverrides(cfg *Config) {
	// 服务器
	envStr("HOST", &cfg.Server.Host)
	envInt("PORT", &cfg.Server.Port)
	envStr("GIN_MODE", &cfg.Server.Mode)
	envStringList("TRUSTED_PROXIES", &cfg.Server.TrustedProxies)

	// 数据库
	envStr("DB_HOST", &cfg.Database.Host)
	envInt("DB_PORT", &cfg.Database.Port)
	envStr("DB_USER", &cfg.Database.User)
	envStr("DB_PASSWORD", &cfg.Database.Password)
	envStr("DB_NAME", &cfg.Database.DBName)
	envStr("DB_SSLMODE", &cfg.Database.SSLMode)

	// Redis
	envStr("REDIS_HOST", &cfg.Redis.Host)
	envInt("REDIS_PORT", &cfg.Redis.Port)
	envStr("REDIS_PASSWORD", &cfg.Redis.Password)
	envInt("REDIS_DB", &cfg.Redis.DB)
	envBool("REDIS_TLS", &cfg.Redis.TLS)
	envStr("REDIS_TLS_SERVER_NAME", &cfg.Redis.TLSServerName)

	// JWT
	envStr("JWT_SECRET", &cfg.JWT.Secret)
	envInt("JWT_EXPIRE_HOUR", &cfg.JWT.ExpireHour)

	// 日志
	envStr("LOG_LEVEL", &cfg.Log.Level)
	envStr("LOG_FORMAT", &cfg.Log.Format)

	// 安全
	envStr("API_KEY_SECRET", &cfg.Security.APIKeySecret)

	// 插件
	envStr("PLUGINS_DIR", &cfg.Plugins.Dir)
	envStr("PLUGINS_MARKETPLACE_GITHUB_TOKEN", &cfg.Plugins.Marketplace.GithubToken)
}

// envStr 如果环境变量存在，覆盖目标字符串
func envStr(key string, dst *string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

// envStringList 用逗号分隔的环境变量覆盖字符串列表。
func envStringList(key string, dst *[]string) {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if value := strings.TrimSpace(part); value != "" {
				values = append(values, value)
			}
		}
		*dst = values
	}
}

// envInt 如果环境变量存在且为合法整数，覆盖目标整数
func envInt(key string, dst *int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

// envBool 如果环境变量存在且为常见布尔值，覆盖目标布尔。
func envBool(key string, dst *bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "true", "1", "yes", "on":
		*dst = true
	case "false", "0", "no", "off":
		*dst = false
	}
}

// ConfigPath 返回配置文件路径
func ConfigPath() string {
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		return v
	}
	return "config.yaml"
}
