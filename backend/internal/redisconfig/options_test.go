package redisconfig

import (
	"testing"

	"github.com/DevilGenius/airgate-core/internal/config"
)

func TestOptionsSetsTLSServerName(t *testing.T) {
	opts := Options(config.RedisConfig{Host: "10.0.0.8", Port: 6379, TLS: true, TLSServerName: "redis.service.local"})
	if opts.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	if opts.TLSConfig.ServerName != "redis.service.local" {
		t.Fatalf("ServerName = %q, want redis.service.local", opts.TLSConfig.ServerName)
	}
}

func TestOptionsDefaultsTLSServerNameToHost(t *testing.T) {
	opts := Options(config.RedisConfig{Host: "redis.local", Port: 6379, TLS: true})
	if opts.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	if opts.TLSConfig.ServerName != "redis.local" {
		t.Fatalf("ServerName = %q, want redis.local", opts.TLSConfig.ServerName)
	}
}
