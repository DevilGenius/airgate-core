package dashboard

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrendReadsIndependentProjectionsConcurrently(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	defer close(release)
	wait := func(ctx context.Context) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	service := NewService(dashboardStubRepository{
		listTrendLogs:       func(ctx context.Context, _, _ time.Time) ([]TrendLog, error) { return nil, wait(ctx) },
		listAPIKeyTrendLogs: func(ctx context.Context, _, _ time.Time) ([]APIKeyTrendLog, error) { return nil, wait(ctx) },
		loadDistributionStats: func(ctx context.Context, _, _ time.Time, _ int) (DistributionSnapshot, error) {
			return DistributionSnapshot{}, wait(ctx)
		},
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := service.loadTrendFresh(ctx, TrendQuery{}, time.UTC, time.Now(), time.Now())
		completed <- err
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("independent projections were serialized")
		}
	}
	cancel()
	if err := <-completed; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestTrendSharesInFlightLoadDespiteCancelledCaller(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var calls atomic.Int32
	service := NewService(dashboardStubRepository{
		listTrendLogs: func(ctx context.Context, _, _ time.Time) ([]TrendLog, error) {
			calls.Add(1)
			entered <- struct{}{}
			select {
			case <-release:
				return []TrendLog{{Model: "shared", Requests: 2}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})
	now := time.Date(2026, 10, 3, 12, 1, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	query := TrendQuery{Range: "90d", Granularity: "day", TZ: "UTC"}
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := service.Trend(ctx, query); first <- err }()
	<-entered
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller error = %v", err)
	}
	// Register a second waiter synchronously on exactly the same work key.
	start, end := resolveTrendTimeRange(query, now)
	second := service.trendFlight.DoChan(trendCacheKey(query, time.UTC, start, end), func() (any, error) {
		return nil, errors.New("cancelled caller discarded shared load")
	})
	close(release)
	result := <-second
	if result.Err != nil || !result.Shared || calls.Load() != 1 {
		t.Fatalf("shared result err=%v, shared=%v, loads=%d", result.Err, result.Shared, calls.Load())
	}
	if result.Val.(Trend).ModelDistribution[0].Requests != 2 {
		t.Fatal("shared result lost request totals")
	}
}
