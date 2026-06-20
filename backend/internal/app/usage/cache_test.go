package usage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
)

func TestNewServiceAcceptsRedisClient(t *testing.T) {
	rdb, _ := redismock.NewClientMock()
	service := NewService(&stubUsageRepository{}, rdb)
	if service.rdb != rdb {
		t.Fatalf("NewService did not keep redis client")
	}
}

func TestStatsSummaryErrorBranches(t *testing.T) {
	repoErr := errors.New("summary failed")
	repo := &stubUsageRepository{
		summaryUserFn:  func(context.Context, int64, StatsFilter) (Summary, error) { return Summary{}, repoErr },
		summaryAdminFn: func(context.Context, StatsFilter) (Summary, error) { return Summary{}, repoErr },
	}
	if _, err := NewService(repo).UserStatsWithModels(t.Context(), 1, StatsFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("UserStatsWithModels() error = %v", err)
	}
	if _, err := NewService(repo).AdminStats(t.Context(), StatsFilter{}, "model"); !errors.Is(err, repoErr) {
		t.Fatalf("AdminStats() error = %v", err)
	}
	emptyResult, err := NewService(&stubUsageRepository{
		summaryAdminFn: func(context.Context, StatsFilter) (Summary, error) { return Summary{TotalRequests: 3}, nil },
	}).AdminStats(t.Context(), StatsFilter{}, "")
	if err != nil || emptyResult.TotalRequests != 3 {
		t.Fatalf("AdminStats(empty groupBy) = %+v err=%v", emptyResult, err)
	}
	if got := normalizeStatsGroupBy(""); got != "" {
		t.Fatalf("normalizeStatsGroupBy(empty) = %q", got)
	}
	if key := usageCacheKey("bad-kind", func() {}); !strings.HasPrefix(key, usageCacheKeyPrefix+":bad:kind:") {
		t.Fatalf("usageCacheKey fallback = %q", key)
	}
}

func TestUsageRedisCacheHelpers(t *testing.T) {
	t.Run("load hit invalid and miss", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("hit").SetVal("7")
		if got, ok := usageLoadCache[int](t.Context(), rdb, "hit"); !ok || got != 7 {
			t.Fatalf("usageLoadCache hit = %d/%v", got, ok)
		}
		mock.ExpectGet("bad").SetVal("{")
		mock.ExpectDel("bad").SetVal(1)
		if _, ok := usageLoadCache[int](t.Context(), rdb, "bad"); ok {
			t.Fatal("usageLoadCache bad json ok = true")
		}
		mock.ExpectGet("miss").RedisNil()
		if _, ok := usageLoadCache[int](t.Context(), rdb, "miss"); ok {
			t.Fatal("usageLoadCache miss ok = true")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("store cache and marshal error", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectSet("key", []byte("7"), time.Second).SetVal("OK")
		usageStoreCache(rdb, "key", 7, time.Second)
		usageStoreCache(rdb, "ignored", func() {}, time.Second)
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("lock states and release", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(true)
		token, ok, busy := usageTryCacheLock(t.Context(), rdb, "key")
		if !ok || busy || token == "" {
			t.Fatalf("lock success token=%q ok=%v busy=%v", token, ok, busy)
		}
		mock.ExpectEvalSha(usageCacheLockReleaseScript.Hash(), []string{"key:lock"}, token).SetVal(int64(1))
		usageReleaseCacheLock("key", token, rdb)

		mock.Regexp().ExpectSetNX("busy:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(false)
		if token, ok, busy := usageTryCacheLock(t.Context(), rdb, "busy"); token != "" || ok || !busy {
			t.Fatalf("lock busy token=%q ok=%v busy=%v", token, ok, busy)
		}
		mock.Regexp().ExpectSetNX("err:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetErr(errors.New("redis failed"))
		if token, ok, busy := usageTryCacheLock(t.Context(), rdb, "err"); token != "" || ok || busy {
			t.Fatalf("lock error token=%q ok=%v busy=%v", token, ok, busy)
		}
		usageReleaseCacheLock("key", "", rdb)
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})
}

func TestUsageCachedResultRedisPaths(t *testing.T) {
	t.Run("cache hit", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").SetVal("11")
		got, err := usageCachedResult[int](t.Context(), rdb, "key", time.Second, func(context.Context) (int, error) {
			t.Fatal("loader should not be called")
			return 0, nil
		})
		if err != nil || got != 11 {
			t.Fatalf("cached result = %d/%v", got, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("lock success stores result", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(true)
		mock.ExpectGet("key").RedisNil()
		mock.ExpectSet("key", []byte("9"), time.Second).SetVal("OK")
		mock.Regexp().ExpectEvalSha(usageCacheLockReleaseScript.Hash(), []string{"key:lock"}, `[0-9a-f-]+`).SetVal(int64(1))
		got, err := usageCachedResult[int](t.Context(), rdb, "key", time.Second, func(context.Context) (int, error) {
			return 9, nil
		})
		if err != nil || got != 9 {
			t.Fatalf("cached result = %d/%v", got, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("lock success finds cache after lock", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(true)
		mock.ExpectGet("key").SetVal("17")
		mock.Regexp().ExpectEvalSha(usageCacheLockReleaseScript.Hash(), []string{"key:lock"}, `[0-9a-f-]+`).SetVal(int64(1))
		got, err := usageCachedResult[int](t.Context(), rdb, "key", time.Second, func(context.Context) (int, error) {
			t.Fatal("loader should not be called")
			return 0, nil
		})
		if err != nil || got != 17 {
			t.Fatalf("cached result = %d/%v", got, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("lock success loader error", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		wantErr := errors.New("loader failed")
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(true)
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectEvalSha(usageCacheLockReleaseScript.Hash(), []string{"key:lock"}, `[0-9a-f-]+`).SetVal(int64(1))
		if _, err := usageCachedResult[int](t.Context(), rdb, "key", time.Second, func(context.Context) (int, error) {
			return 0, wantErr
		}); !errors.Is(err, wantErr) {
			t.Fatalf("cached result error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("busy cache appears", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(false)
		mock.ExpectGet("key").SetVal("13")
		got, err := usageCachedResult[int](t.Context(), rdb, "key", time.Second, func(context.Context) (int, error) {
			t.Fatal("loader should not be called")
			return 0, nil
		})
		if err != nil || got != 13 {
			t.Fatalf("cached result = %d/%v", got, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("lock error falls back and stores", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetErr(errors.New("redis failed"))
		mock.ExpectSet("key", []byte("15"), time.Second).SetVal("OK")
		got, err := usageCachedResult[int](t.Context(), rdb, "key", time.Second, func(context.Context) (int, error) {
			return 15, nil
		})
		if err != nil || got != 15 {
			t.Fatalf("cached result = %d/%v", got, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("fallback loader error", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		wantErr := errors.New("fallback failed")
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetErr(errors.New("redis failed"))
		if _, err := usageCachedResult[int](t.Context(), rdb, "key", time.Second, func(context.Context) (int, error) {
			return 0, wantErr
		}); !errors.Is(err, wantErr) {
			t.Fatalf("cached result error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})
}

func TestUsageWaitForCacheTimeoutAndCanceled(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	mock.ExpectGet("timeout").RedisNil()
	if _, ok := usageWaitForCache[int](t.Context(), rdb, "timeout", 0); ok {
		t.Fatal("usageWaitForCache timeout ok = true")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	mock.ExpectGet("canceled").RedisNil()
	if _, ok := usageWaitForCache[int](ctx, rdb, "canceled", time.Second); ok {
		t.Fatal("usageWaitForCache canceled ok = true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestUsageWaitForCacheBackoff(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	mock.ExpectGet("slow").RedisNil()
	mock.ExpectGet("slow").RedisNil()
	mock.ExpectGet("slow").RedisNil()
	if _, ok := usageWaitForCache[int](t.Context(), rdb, "slow", 60*time.Millisecond); ok {
		t.Fatal("usageWaitForCache slow ok = true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestBuildTrendBucketsSkipsInvalidTimes(t *testing.T) {
	buckets := BuildTrendBuckets([]TrendEntry{
		{CreatedAt: "not-a-time", InputTokens: 100},
		{CreatedAt: "2026-06-20T01:00:00Z", InputTokens: 1},
	}, "hour", "UTC")
	if len(buckets) != 1 || buckets[0].InputTokens != 1 {
		t.Fatalf("BuildTrendBuckets() = %+v", buckets)
	}
}
