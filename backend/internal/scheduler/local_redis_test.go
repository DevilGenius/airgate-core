package scheduler

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func localReviewRedis(t *testing.T) *redis.Client {
	t.Helper()
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG for isolated local Redis tests")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local configuration failed")
	}
	options := &redis.Options{Addr: fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port), Password: cfg.Redis.Password, DB: cfg.Redis.DB}
	if cfg.Redis.TLS {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.Redis.TLSServerName}
	}
	rdb := redis.NewClient(options)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestLocalRPMRefundDoesNotCreateOrUnderflowCounters(t *testing.T) {
	rdb := localReviewRedis(t)
	key := "ag:review:r17:" + uuid.NewString()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	defer rdb.Del(context.Background(), key)
	for _, seed := range []int{-1, 0, 1, 2} {
		if seed >= 0 {
			if err := rdb.Set(ctx, key, seed, time.Minute).Err(); err != nil {
				t.Fatal(err)
			}
		}
		for range 4 {
			if n, err := decrementRPMScript.Run(ctx, rdb, []string{key}).Int(); err != nil || n < 0 {
				t.Fatalf("refund = %d, %v", n, err)
			}
		}
		if seed == -1 {
			if n, err := rdb.Exists(ctx, key).Result(); err != nil || n != 0 {
				t.Fatal("refund recreated expired window")
			}
		} else if ttl, err := rdb.TTL(ctx, key).Result(); err != nil || ttl <= 0 {
			t.Fatal("refund lost window expiry")
		}
	}
}
