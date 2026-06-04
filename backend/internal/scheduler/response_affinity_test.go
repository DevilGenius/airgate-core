package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestResponseAffinityMemoryFallback(t *testing.T) {
	t.Parallel()

	store := NewResponseAffinity(nil)
	store.Bind(context.Background(), 7, "openai", "resp_123", 42)

	accountID, ok := store.Get(context.Background(), 7, "openai", "resp_123")
	if !ok {
		t.Fatal("expected memory response affinity hit")
	}
	if accountID != 42 {
		t.Fatalf("accountID = %d, want 42", accountID)
	}
}

func TestResponseAffinityExpiresMemoryEntries(t *testing.T) {
	t.Parallel()

	store := NewResponseAffinity(nil)
	store.ttl = time.Millisecond
	store.Bind(context.Background(), 7, "openai", "resp_expired", 42)
	time.Sleep(2 * time.Millisecond)

	if accountID, ok := store.Get(context.Background(), 7, "openai", "resp_expired"); ok {
		t.Fatalf("expected expired miss, got accountID %d", accountID)
	}
}

func TestResponseAffinityRefreshThrottlesRedisTTLRefresh(t *testing.T) {
	t.Parallel()

	store := NewResponseAffinity(nil)
	key := responseAffinityKey(7, "openai", "resp_refresh")
	store.setMemory(key, 42)

	if shouldRefreshRedis := store.refreshMemory(key, 42); shouldRefreshRedis {
		t.Fatal("refreshMemory() = true, want false before redis refresh interval")
	}

	store.mu.Lock()
	binding := store.items[key]
	binding.redisRefreshAfter = time.Now().Add(-time.Second)
	store.items[key] = binding
	store.mu.Unlock()

	if shouldRefreshRedis := store.refreshMemory(key, 42); !shouldRefreshRedis {
		t.Fatal("refreshMemory() = false, want true after redis refresh interval")
	}
}

func TestResponseAffinityRedisLoadedEntryRefreshesRedisTTLImmediately(t *testing.T) {
	t.Parallel()

	store := NewResponseAffinity(nil)
	key := responseAffinityKey(7, "openai", "resp_from_redis")
	store.setMemoryRefreshDue(key, 42)

	if shouldRefreshRedis := store.refreshMemory(key, 42); !shouldRefreshRedis {
		t.Fatal("refreshMemory() = false, want true for redis-loaded memory entry")
	}
}

func TestStickySessionMemoryFallback(t *testing.T) {
	t.Parallel()

	sticky := NewStickySession(nil)
	sticky.Set(context.Background(), 9, "openai", "session-1", 11)

	accountID, ok := sticky.Get(context.Background(), 9, "openai", "session-1")
	if !ok {
		t.Fatal("expected memory sticky hit")
	}
	if accountID != 11 {
		t.Fatalf("accountID = %d, want 11", accountID)
	}
}

func TestStickySessionSetThrottlesRedisTTLRefresh(t *testing.T) {
	t.Parallel()

	sticky := NewStickySession(nil)
	key := stickyKey(9, "openai", "session-refresh")

	if shouldRefreshRedis := sticky.refreshMemory(key, 11); !shouldRefreshRedis {
		t.Fatal("refreshMemory() = false, want true for new sticky binding")
	}
	if shouldRefreshRedis := sticky.refreshMemory(key, 11); shouldRefreshRedis {
		t.Fatal("refreshMemory() = true, want false before redis refresh interval")
	}

	sticky.mu.Lock()
	binding := sticky.items[key]
	binding.redisRefreshAfter = time.Now().Add(-time.Second)
	sticky.items[key] = binding
	sticky.mu.Unlock()

	if shouldRefreshRedis := sticky.refreshMemory(key, 11); !shouldRefreshRedis {
		t.Fatal("refreshMemory() = false, want true after redis refresh interval")
	}
}

func TestStickySessionRedisLoadedEntryRefreshesRedisTTLImmediately(t *testing.T) {
	t.Parallel()

	sticky := NewStickySession(nil)
	key := stickyKey(9, "openai", "session-from-redis")
	sticky.setMemoryRefreshDue(key, 11)

	if shouldRefreshRedis := sticky.refreshMemory(key, 11); !shouldRefreshRedis {
		t.Fatal("refreshMemory() = false, want true for redis-loaded sticky entry")
	}
}
