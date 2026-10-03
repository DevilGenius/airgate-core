package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
)

func TestTrendIncludesDistributionsAndCachesThem(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	want := DistributionSnapshot{
		Accounts: []DistributionStats{{ID: 31, Name: "credential", Requests: 8, Tokens: 123, ActualCost: 1.25, StandardCost: 2.5}},
		Groups:   []DistributionStats{{ID: 7, Name: "group", Requests: 8, Tokens: 123, ActualCost: 1.25, StandardCost: 2.5}},
	}
	calls := 0
	repo := dashboardStubRepository{
		loadDistributionStats: func(_ context.Context, start, end time.Time, userID int) (DistributionSnapshot, error) {
			calls++
			if userID != 42 || !start.Equal(now.Add(-12*time.Hour)) || !end.Equal(now) {
				t.Fatalf("distribution bounds/filter = %s, %s, %d", start, end, userID)
			}
			return want, nil
		},
	}
	service := NewService(repo)
	service.now = func() time.Time { return now }
	query := TrendQuery{Range: "today", Granularity: "hour", UserID: 42, TZ: "UTC"}
	trend, err := service.Trend(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trend.AccountDistribution, want.Accounts) || !reflect.DeepEqual(trend.GroupDistribution, want.Groups) {
		t.Fatalf("distributions = %+v", trend)
	}

	rdb, mock := redismock.NewClientMock()
	defer func() { _ = rdb.Close() }()
	service.rdb = rdb
	key := trendCacheKey(query, time.UTC, now.Add(-12*time.Hour), now)
	raw, err := json.Marshal(trend)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectSet(key, raw, trendCacheTTL).SetVal("OK")
	service.storeTrendCache(t.Context(), key, trend)
	mock.ExpectGet(key).SetVal(string(raw))
	cached, err := service.Trend(t.Context(), query)
	if err != nil || !reflect.DeepEqual(cached, trend) || calls != 1 {
		t.Fatalf("cached trend = %+v, err = %v, distribution calls = %d", cached, err, calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTrendReturnsDistributionError(t *testing.T) {
	wantErr := errors.New("rollup history unavailable")
	service := NewService(dashboardStubRepository{
		loadDistributionStats: func(context.Context, time.Time, time.Time, int) (DistributionSnapshot, error) {
			return DistributionSnapshot{}, wantErr
		},
	})
	if _, err := service.Trend(t.Context(), TrendQuery{Range: "today", TZ: "UTC"}); !errors.Is(err, wantErr) {
		t.Fatalf("Trend error = %v, want %v", err, wantErr)
	}
}
