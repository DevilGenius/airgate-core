package redisconfig

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/internal/config"
)

func Options(cfg config.RedisConfig) *redis.Options {
	opts := &redis.Options{
		Addr:                  fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:              cfg.Password,
		DB:                    cfg.DB,
		DialTimeout:           time.Second,
		ReadTimeout:           time.Second,
		WriteTimeout:          time.Second,
		PoolTimeout:           time.Second,
		MaxRetries:            -1,
		ContextTimeoutEnabled: true,
	}
	if cfg.TLS {
		serverName := strings.TrimSpace(cfg.TLSServerName)
		if serverName == "" {
			serverName = strings.TrimSpace(cfg.Host)
		}
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	}
	return opts
}
