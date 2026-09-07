package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
)

func TestKeyCacheBoundsNegativeEntriesAndExpiresWithoutAccess(t *testing.T) {
	cache := newLocalKeyCache(8)
	defer cache.Clear()
	for i := range 100 {
		cache.Store(fmt.Sprint(i), apiKeyCacheEntry{err: ErrInvalidAPIKey, expiresAt: time.Now().Add(10 * time.Millisecond)})
	}
	count := func() int { n := 0; cache.Range(func(any, any) bool { n++; return true }); return n }
	if count() != 8 {
		t.Fatalf("cache capacity = %d", count())
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for count() > 0 {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("unused negative entries were not reclaimed")
		}
	}
}

func TestAPIKeyValidationCoalescesConcurrentDatabaseMisses(t *testing.T) {
	resetAPIKeyTestCache(t)
	previousQuery, previousGroup := queryAPIKeyForValidation, apiKeyValidations
	defer func() { queryAPIKeyForValidation = previousQuery; apiKeyValidations = previousGroup }()
	apiKeyValidations = newKeyValidationGroup(8, 256)
	var calls atomic.Int64
	queryAPIKeyForValidation = func(context.Context, *ent.Client, string) (*ent.APIKey, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &ent.APIKey{ID: 42, Edges: ent.APIKeyEdges{User: &ent.User{ID: 1}, Group: &ent.Group{ID: 2}}}, nil
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 64 {
		workers.Go(func() {
			<-start
			info, err := ValidateAPIKey(t.Context(), nil, "sk-coalesced-request")
			if err != nil || info == nil || info.KeyID != 42 {
				t.Errorf("validation = %+v, %v", info, err)
			}
		})
	}
	close(start)
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("database lookups = %d", calls.Load())
	}
}

func TestValidationBudgetRejectsNewKeysAndAllowsWaiterCancellation(t *testing.T) {
	g := newKeyValidationGroup(1, 3)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = g.do(t.Context(), "held", func(context.Context) (*APIKeyInfo, error) {
			close(entered)
			<-release
			return &APIKeyInfo{KeyID: 1}, nil
		})
	}()
	<-entered
	loader := func(context.Context) (*APIKeyInfo, error) {
		t.Error("rejected request reached loader")
		return nil, nil
	}
	if _, err := g.do(t.Context(), "another", loader); !errors.Is(err, ErrAPIKeyLookupBusy) {
		t.Errorf("full budget = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := g.do(ctx, "held", loader); !errors.Is(err, context.Canceled) {
		t.Errorf("waiter cancel = %v", err)
	}
	close(release)
	<-done
	for range 3 {
		if !g.allowDatabaseLookup() {
			t.Fatal("rate budget rejected too early")
		}
	}
	if g.allowDatabaseLookup() {
		t.Fatal("rate budget exceeded")
	}
	for _, key := range []string{"sk-", "sk-with space", "sk-" + strings.Repeat("x", 257)} {
		if _, err := ValidateAPIKey(t.Context(), nil, key); !errors.Is(err, ErrInvalidAPIKey) {
			t.Errorf("malformed key reached database: %v", err)
		}
	}
}
