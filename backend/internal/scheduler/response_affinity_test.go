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
