package usage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type blockingPageRepository struct {
	started chan ListFilter
	finish  chan struct{}
	calls   atomic.Int32
}

func (r *blockingPageRepository) BuildPageIndex(ctx context.Context, filter ListFilter) (*PageIndex, error) {
	r.calls.Add(1)
	r.started <- filter
	select {
	case <-r.finish:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	index := &PageIndex{}
	if err := index.AddID(130); err != nil {
		return nil, err
	}
	return index, nil
}

func TestPaginationBuildIsIndependentCoalescedAndBounded(t *testing.T) {
	repo := &blockingPageRepository{started: make(chan ListFilter, 2), finish: make(chan struct{})}
	defer close(repo.finish)
	cache := newPaginationCache(repo, nil)
	filter, _ := normalizedListFilter(ListFilter{})
	started := time.Now()
	if info := cache.info(t.Context(), filter, false); info.Status != "preparing" {
		t.Fatalf("info=%+v", info)
	}
	if time.Since(started) > time.Second {
		t.Fatal("metadata waited for full index")
	}
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("builder not started")
	}
	for range 100 {
		if info := cache.info(t.Context(), filter, false); info.Status != "preparing" {
			t.Fatalf("info=%+v", info)
		}
	}
	other := filter
	other.Model = "different"
	if info := cache.info(t.Context(), other, false); info.Status != "preparing" {
		t.Fatalf("queued info=%+v", info)
	}
	if repo.calls.Load() != 1 || len(cache.builds) != 1 {
		t.Fatal("parallel or unbounded index builds")
	}
}

func TestFailedRefreshCanRetryWhileOldViewRemainsReadable(t *testing.T) {
	repo := &blockingPageRepository{started: make(chan ListFilter, 2), finish: make(chan struct{})}
	defer close(repo.finish)
	cache := newPaginationCache(repo, nil)
	filter, _ := normalizedListFilter(ListFilter{})
	key := pageIndexFilterKey(filter)
	index := &PageIndex{Token: "old", FilterKey: key, CreatedAt: time.Now().Add(-2 * pageIndexFreshTTL), ExpiresAt: time.Now().Add(time.Hour)}
	cache.put(index)
	cache.builds[key] = pageIndexBuild{startedAt: time.Now().Add(-2 * pageIndexRetryDelay), failed: true}
	info := cache.info(t.Context(), filter, true)
	if info.Status != "ready" || !info.Refreshing || info.Snapshot != index.Token {
		t.Fatalf("refresh=%+v", info)
	}
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("failed refresh never retried")
	}
}
