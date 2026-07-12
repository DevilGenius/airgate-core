package usage

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"golang.org/x/sync/singleflight"
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
	if _, err := NewService(repo).AdminStats(t.Context(), StatsFilter{}, "model", true); !errors.Is(err, repoErr) {
		t.Fatalf("AdminStats() error = %v", err)
	}
	emptyResult, err := NewService(&stubUsageRepository{
		summaryAdminFn: func(context.Context, StatsFilter) (Summary, error) { return Summary{TotalRequests: 3}, nil },
	}).AdminStats(t.Context(), StatsFilter{}, "", true)
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

	t.Run("publish cache atomically and reject invalid value", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectEvalSha(
			usageCachePublishScript.Hash(),
			[]string{"key:lock", "key"},
			"token",
			[]byte("7"),
			time.Second.Milliseconds(),
		).SetVal(int64(1))
		if !usagePublishCache(rdb, "key", "token", 7, time.Second) {
			t.Fatal("usagePublishCache success = false")
		}
		if usagePublishCache(rdb, "ignored", "token", func() {}, time.Second) {
			t.Fatal("usagePublishCache marshal error = true")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("publish requires current lock token", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectEvalSha(
			usageCachePublishScript.Hash(),
			[]string{"key:lock", "key"},
			"stale-token",
			[]byte("7"),
			time.Second.Milliseconds(),
		).SetVal(int64(0))
		if usagePublishCache(rdb, "key", "stale-token", 7, time.Second) {
			t.Fatal("usagePublishCache stale token = true")
		}
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
		mock.Regexp().ExpectEvalSha(
			usageCachePublishScript.Hash(),
			[]string{"key:lock", "key"},
			`[0-9a-f-]+`,
			[]byte("9"),
			time.Second.Milliseconds(),
		).SetVal(int64(1))
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

	t.Run("lost lock does not let old owner overwrite cache", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(true)
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectEvalSha(
			usageCachePublishScript.Hash(),
			[]string{"key:lock", "key"},
			`[0-9a-f-]+`,
			[]byte("9"),
			time.Second.Milliseconds(),
		).SetVal(int64(0))
		mock.Regexp().ExpectEvalSha(usageCacheLockReleaseScript.Hash(), []string{"key:lock"}, `[0-9a-f-]+`).SetVal(int64(0))
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

	t.Run("publish error returns value without fallback write", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(true)
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectEvalSha(
			usageCachePublishScript.Hash(),
			[]string{"key:lock", "key"},
			`[0-9a-f-]+`,
			[]byte("9"),
			time.Second.Milliseconds(),
		).SetErr(errors.New("publish failed"))
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

	t.Run("busy waiter does not run duplicate loader", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetVal(false)
		mock.ExpectGet("key").RedisNil()
		if _, err := usageCachedResult[int](ctx, rdb, "key", time.Second, func(context.Context) (int, error) {
			t.Fatal("loader should not run while another owner holds the lock")
			return 0, nil
		}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("busy cached result error = %v, want deadline exceeded", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})

	t.Run("lock error computes without publishing", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectGet("key").RedisNil()
		mock.Regexp().ExpectSetNX("key:lock", `[0-9a-f-]+`, usageCacheLockTTL).SetErr(errors.New("redis failed"))
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

func TestUsageCachedResultWithFlightDetachesSharedLoadFromOwnerCancellation(t *testing.T) {
	type contextKey struct{}

	var loaderCalls atomic.Int32
	loaderStarted := make(chan struct{})
	releaseLoader := make(chan struct{})
	loader := func(ctx context.Context) (int, error) {
		if loaderCalls.Add(1) != 1 {
			return 0, errors.New("loader called more than once")
		}
		close(loaderStarted)
		if got := ctx.Value(contextKey{}); got != "owner-value" {
			return 0, errors.New("loader context value was not preserved")
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			return 0, errors.New("loader context has no hard deadline")
		}
		remaining := time.Until(deadline)
		if remaining < usageCacheLoadTimeout-time.Second || remaining > usageCacheLoadTimeout {
			return 0, errors.New("loader context hard deadline is invalid")
		}
		<-releaseLoader
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 23, nil
	}

	flight := &singleflight.Group{}
	ownerBase := context.WithValue(t.Context(), contextKey{}, "owner-value")
	ownerDeadlineCtx, cancelDeadline := context.WithTimeout(ownerBase, 100*time.Millisecond)
	defer cancelDeadline()
	ownerCtx, cancelOwner := context.WithCancel(ownerDeadlineCtx)
	ownerResult := make(chan error, 1)
	go func() {
		_, err := usageCachedResultWithFlight(ownerCtx, flight, nil, "key", time.Second, loader)
		ownerResult <- err
	}()
	select {
	case <-loaderStarted:
	case <-time.After(time.Second):
		t.Fatal("shared loader did not start")
	}

	waiterResult := make(chan struct {
		value int
		err   error
	}, 1)
	go func() {
		value, err := usageCachedResultWithFlight(t.Context(), flight, nil, "key", time.Second, loader)
		waiterResult <- struct {
			value int
			err   error
		}{value: value, err: err}
	}()

	// Give the waiter time to join the still-running shared call before the
	// owner returns. The loader remains blocked until after cancellation.
	time.Sleep(25 * time.Millisecond)
	cancelOwner()
	select {
	case err := <-ownerResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("owner error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not stop waiting after cancellation")
	}
	close(releaseLoader)
	select {
	case result := <-waiterResult:
		if result.err != nil || result.value != 23 {
			t.Fatalf("waiter result = %d/%v", result.value, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not receive shared result")
	}
	if got := loaderCalls.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1", got)
	}
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
