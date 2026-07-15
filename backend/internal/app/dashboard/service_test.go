package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
)

func TestStatsComputesDerivedMetrics(t *testing.T) {
	service := NewService(dashboardStubRepository{
		loadStatsSnapshot: func(_ context.Context, _, _ time.Time) (StatsSnapshot, error) {
			return StatsSnapshot{
				TodayRequests:           6,
				TodayImageRequests:      2,
				TodayNonImageRequests:   4,
				TodayNonImageDurationMs: 1000,
				TodayFirstEventRequests: 2,
				TodayFirstEventMs:       300,
				TodayFirstTokenRequests: 2,
				TodayFirstTokenMs:       500,
				TodayImageDurationMs:    240000,
				RecentRequests:          10,
				RecentTokens:            500,
			}, nil
		},
	})

	result, err := service.Stats(t.Context(), 0, "")
	if err != nil {
		t.Fatalf("Stats() returned error: %v", err)
	}
	if result.AvgDurationMs != 250 {
		t.Fatalf("AvgDurationMs = %v, want 250", result.AvgDurationMs)
	}
	if result.AvgFirstEventMs != 150 {
		t.Fatalf("AvgFirstEventMs = %v, want 150", result.AvgFirstEventMs)
	}
	if result.AvgFirstTokenMs != 250 {
		t.Fatalf("AvgFirstTokenMs = %v, want 250", result.AvgFirstTokenMs)
	}
	if result.AvgImageDurationMs != 120000 {
		t.Fatalf("AvgImageDurationMs = %v, want 120000", result.AvgImageDurationMs)
	}
	if result.TodayImageRequests != 2 {
		t.Fatalf("TodayImageRequests = %v, want 2", result.TodayImageRequests)
	}
	if result.RPM != 2 {
		t.Fatalf("RPM = %v, want 2", result.RPM)
	}
	if result.TPM != 100 {
		t.Fatalf("TPM = %v, want 100", result.TPM)
	}
}

func TestStatsReturnsRepositoryError(t *testing.T) {
	repoErr := errors.New("stats failed")
	service := NewService(dashboardStubRepository{
		loadStatsSnapshot: func(context.Context, time.Time, time.Time) (StatsSnapshot, error) {
			return StatsSnapshot{}, repoErr
		},
	})
	if _, err := service.Stats(t.Context(), 7, "UTC"); !errors.Is(err, repoErr) {
		t.Fatalf("Stats() error = %v, want %v", err, repoErr)
	}
}

func TestResolveTrendTimeRangeCustomIncludesEndDate(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)

	start, end := resolveTrendTimeRange(TrendQuery{
		Range:     "custom",
		StartDate: "2026-03-01",
		EndDate:   "2026-03-15",
	}, now)

	if !start.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("start = %v, want 2026-03-01", start)
	}
	if !end.Equal(time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("end = %v, want 2026-03-16", end)
	}
}

func TestResolveTrendTimeRangePresetUsesNaturalDayWindow(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		rangeName string
		wantStart time.Time
	}{
		{
			name:      "seven days",
			rangeName: "7d",
			wantStart: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "thirty days",
			rangeName: "30d",
			wantStart: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "ninety days",
			rangeName: "90d",
			wantStart: time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := resolveTrendTimeRange(TrendQuery{Range: tt.rangeName}, now)
			if !start.Equal(tt.wantStart) {
				t.Fatalf("start = %v, want %v", start, tt.wantStart)
			}
			if !end.Equal(now) {
				t.Fatalf("end = %v, want %v", end, now)
			}
		})
	}
}

func TestResolveTrendTimeRangeDefault(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	start, end := resolveTrendTimeRange(TrendQuery{Range: "unknown"}, now)
	if !start.Equal(time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)) || !end.Equal(now) {
		t.Fatalf("default range = %v %v", start, end)
	}
}

func TestTrendCacheKeyBucketsMovingEndTime(t *testing.T) {
	query := TrendQuery{Range: "today", Granularity: "hour", UserID: 7}
	start := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	key1 := trendCacheKey(query, time.UTC, start, time.Date(2026, 5, 27, 12, 0, 1, 0, time.UTC))
	key2 := trendCacheKey(query, time.UTC, start, time.Date(2026, 5, 27, 12, 0, 14, 0, time.UTC))
	key3 := trendCacheKey(query, time.UTC, start, time.Date(2026, 5, 27, 12, 0, 16, 0, time.UTC))

	if key1 != key2 {
		t.Fatalf("same cache bucket keys differ: %q vs %q", key1, key2)
	}
	if key1 == key3 {
		t.Fatalf("different cache bucket keys unexpectedly match: %q", key1)
	}

	otherUser := query
	otherUser.UserID = 8
	if key1 == trendCacheKey(otherUser, time.UTC, start, time.Date(2026, 5, 27, 12, 0, 1, 0, time.UTC)) {
		t.Fatal("cache key must include user scope")
	}
}

func TestTrendAggregatesTopUsersAndBuckets(t *testing.T) {
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	service := NewService(dashboardStubRepository{
		listTrendLogs: func(_ context.Context, _, _ time.Time) ([]TrendLog, error) {
			return []TrendLog{
				{
					UserID:            1,
					UserEmail:         "a@test.com",
					Model:             "gpt-4.1",
					InputTokens:       10,
					OutputTokens:      20,
					CachedInputTokens: 5,
					ActualCost:        1.2,
					StandardCost:      1.5,
					CreatedAt:         time.Date(2026, 4, 1, 10, 15, 0, 0, time.UTC),
				},
				{
					UserID:            1,
					UserEmail:         "a@test.com",
					Model:             "gpt-4.1",
					InputTokens:       2,
					OutputTokens:      3,
					CachedInputTokens: 0,
					ActualCost:        0.2,
					StandardCost:      0.3,
					CreatedAt:         time.Date(2026, 4, 1, 10, 45, 0, 0, time.UTC),
				},
				{
					UserID:            2,
					UserEmail:         "b@test.com",
					Model:             "gpt-4o",
					InputTokens:       5,
					OutputTokens:      5,
					CachedInputTokens: 0,
					ActualCost:        0.5,
					StandardCost:      0.8,
					CreatedAt:         time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC),
				},
			}, nil
		},
	})
	service.now = func() time.Time { return now }

	result, err := service.Trend(t.Context(), TrendQuery{Range: "today", Granularity: "hour"})
	if err != nil {
		t.Fatalf("Trend() returned error: %v", err)
	}
	if len(result.ModelDistribution) != 2 {
		t.Fatalf("len(ModelDistribution) = %d, want 2", len(result.ModelDistribution))
	}
	if result.ModelDistribution[0].Model != "gpt-4.1" || result.ModelDistribution[0].Requests != 2 {
		t.Fatalf("unexpected first model stat: %+v", result.ModelDistribution[0])
	}
	if len(result.TokenTrend) != 2 {
		t.Fatalf("len(TokenTrend) = %d, want 2", len(result.TokenTrend))
	}
	if len(result.TopUsers) == 0 || result.TopUsers[0].UserID != 1 {
		t.Fatalf("unexpected top users: %+v", result.TopUsers)
	}
}

func TestTrendReturnsCachedValue(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	query := TrendQuery{Range: "today", Granularity: "day", TZ: "UTC"}
	start, end := resolveTrendTimeRange(query, now)
	key := trendCacheKey(query, time.UTC, start, end)
	cached := Trend{ModelDistribution: []ModelStats{{Model: "cached", Requests: 1}}}
	raw, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("marshal cached trend: %v", err)
	}
	mock.ExpectGet(key).SetVal(string(raw))

	service := NewService(dashboardStubRepository{
		listTrendLogs: func(context.Context, time.Time, time.Time) ([]TrendLog, error) {
			t.Fatalf("cache hit should not load fresh trend")
			return nil, nil
		},
	}, rdb)
	service.now = func() time.Time { return now }

	got, err := service.Trend(t.Context(), query)
	if err != nil {
		t.Fatalf("Trend() cache hit error: %v", err)
	}
	if len(got.ModelDistribution) != 1 || got.ModelDistribution[0].Model != "cached" {
		t.Fatalf("cached trend = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestTrendLockOwnerLoadsFreshAndStoresCache(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	query := TrendQuery{Range: "today", Granularity: "day", TZ: "UTC"}
	start, end := resolveTrendTimeRange(query, now)
	key := trendCacheKey(query, time.UTC, start, end)

	oldToken := newTrendLockToken
	newTrendLockToken = func() string { return "token" }
	defer func() { newTrendLockToken = oldToken }()

	mock.ExpectGet(key).RedisNil()
	mock.ExpectSetNX(key+":lock", "token", trendLockTTL).SetVal(true)
	mock.ExpectGet(key).RedisNil()
	logs := []TrendLog{{UserID: 1, UserEmail: "a@test.com", Model: "gpt", InputTokens: 1, OutputTokens: 2, ActualCost: 0.1, StandardCost: 0.2, CreatedAt: now}}
	expectedTrend := Trend{
		ModelDistribution: aggregateModelDistribution(logs),
		UserRanking:       aggregateUserRanking(logs),
		TokenTrend:        aggregateTokenTrend(logs, query.Granularity, time.UTC),
		TopUsers:          aggregateTopUsers(logs, query.Granularity, time.UTC),
	}
	expectedRaw, err := json.Marshal(expectedTrend)
	if err != nil {
		t.Fatalf("marshal expected trend: %v", err)
	}
	mock.ExpectSet(key, expectedRaw, trendCacheTTL).SetVal("OK")
	mock.ExpectEvalSha(trendLockReleaseScript.Hash(), []string{key + ":lock"}, "token").SetVal(int64(1))

	service := NewService(dashboardStubRepository{
		listTrendLogs: func(context.Context, time.Time, time.Time) ([]TrendLog, error) {
			return logs, nil
		},
	}, rdb)
	service.now = func() time.Time { return now }

	got, err := service.Trend(t.Context(), query)
	if err != nil {
		t.Fatalf("Trend() lock owner error: %v", err)
	}
	if len(got.ModelDistribution) != 1 || got.ModelDistribution[0].Model != "gpt" {
		t.Fatalf("fresh trend = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestTrendLockOwnerReturnsCacheLoadedAfterLock(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	query := TrendQuery{Range: "today", Granularity: "day", TZ: "UTC"}
	start, end := resolveTrendTimeRange(query, now)
	key := trendCacheKey(query, time.UTC, start, end)
	cached := Trend{ModelDistribution: []ModelStats{{Model: "locked-cache", Requests: 1}}}
	raw, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("marshal locked cache: %v", err)
	}

	oldToken := newTrendLockToken
	newTrendLockToken = func() string { return "token" }
	defer func() { newTrendLockToken = oldToken }()

	mock.ExpectGet(key).RedisNil()
	mock.ExpectSetNX(key+":lock", "token", trendLockTTL).SetVal(true)
	mock.ExpectGet(key).SetVal(string(raw))
	mock.ExpectEvalSha(trendLockReleaseScript.Hash(), []string{key + ":lock"}, "token").SetVal(int64(1))

	service := NewService(dashboardStubRepository{
		listTrendLogs: func(context.Context, time.Time, time.Time) ([]TrendLog, error) {
			t.Fatalf("second cache hit should not load fresh trend")
			return nil, nil
		},
	}, rdb)
	service.now = func() time.Time { return now }
	got, err := service.Trend(t.Context(), query)
	if err != nil {
		t.Fatalf("Trend() second cache hit: %v", err)
	}
	if got.ModelDistribution[0].Model != "locked-cache" {
		t.Fatalf("Trend() = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestTrendLockOwnerReturnsFreshLoadError(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	query := TrendQuery{Range: "today", Granularity: "day", TZ: "UTC"}
	start, end := resolveTrendTimeRange(query, now)
	key := trendCacheKey(query, time.UTC, start, end)
	repoErr := errors.New("locked trend failed")

	oldToken := newTrendLockToken
	newTrendLockToken = func() string { return "token" }
	defer func() { newTrendLockToken = oldToken }()

	mock.ExpectGet(key).RedisNil()
	mock.ExpectSetNX(key+":lock", "token", trendLockTTL).SetVal(true)
	mock.ExpectGet(key).RedisNil()
	mock.ExpectEvalSha(trendLockReleaseScript.Hash(), []string{key + ":lock"}, "token").SetVal(int64(1))

	service := NewService(dashboardStubRepository{
		listTrendLogs: func(context.Context, time.Time, time.Time) ([]TrendLog, error) {
			return nil, repoErr
		},
	}, rdb)
	service.now = func() time.Time { return now }
	if _, err := service.Trend(t.Context(), query); !errors.Is(err, repoErr) {
		t.Fatalf("Trend() locked fresh error = %v, want %v", err, repoErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestTrendLockBusyWaitsForCache(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	query := TrendQuery{Range: "today", Granularity: "day", TZ: "UTC"}
	start, end := resolveTrendTimeRange(query, now)
	key := trendCacheKey(query, time.UTC, start, end)
	cached := Trend{ModelDistribution: []ModelStats{{Model: "waited", Requests: 1}}}
	raw, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("marshal waited trend: %v", err)
	}
	oldToken := newTrendLockToken
	newTrendLockToken = func() string { return "busy-token" }
	defer func() { newTrendLockToken = oldToken }()
	mock.ExpectGet(key).RedisNil()
	mock.ExpectSetNX(key+":lock", "busy-token", trendLockTTL).SetVal(false)
	mock.ExpectGet(key).SetVal(string(raw))

	service := NewService(dashboardStubRepository{}, rdb)
	service.now = func() time.Time { return now }
	got, err := service.Trend(t.Context(), query)
	if err != nil {
		t.Fatalf("Trend() lock busy cache error: %v", err)
	}
	if len(got.ModelDistribution) != 1 || got.ModelDistribution[0].Model != "waited" {
		t.Fatalf("waited trend = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestTrendFreshErrors(t *testing.T) {
	repoErr := errors.New("trend failed")
	service := NewService(dashboardStubRepository{
		listTrendLogs: func(context.Context, time.Time, time.Time) ([]TrendLog, error) {
			return nil, repoErr
		},
	})
	service.now = func() time.Time { return time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC) }
	if _, err := service.Trend(t.Context(), TrendQuery{Range: "today", TZ: "UTC"}); !errors.Is(err, repoErr) {
		t.Fatalf("Trend() error = %v, want %v", err, repoErr)
	}
}

func TestTrendCacheHelpers(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	service := NewService(dashboardStubRepository{}, rdb)
	trend := Trend{ModelDistribution: []ModelStats{{Model: "cached", Requests: 1}}}
	raw, err := json.Marshal(trend)
	if err != nil {
		t.Fatalf("marshal trend: %v", err)
	}

	mock.ExpectGet("cache-hit").SetVal(string(raw))
	if got, ok := service.loadTrendCache(t.Context(), "cache-hit"); !ok || got.ModelDistribution[0].Model != "cached" {
		t.Fatalf("loadTrendCache hit = %+v %v", got, ok)
	}
	mock.ExpectGet("cache-bad-json").SetVal("{")
	mock.ExpectDel("cache-bad-json").SetVal(1)
	if _, ok := service.loadTrendCache(t.Context(), "cache-bad-json"); ok {
		t.Fatalf("bad json cache should miss")
	}
	mock.ExpectGet("cache-miss").RedisNil()
	if _, ok := service.loadTrendCache(t.Context(), "cache-miss"); ok {
		t.Fatalf("missing cache should miss")
	}
	mock.ExpectSet("cache-store", raw, trendCacheTTL).SetVal("OK")
	service.storeTrendCache(t.Context(), "cache-store", trend)
	service.storeTrendCache(t.Context(), "cache-marshal-error", Trend{ModelDistribution: []ModelStats{{ActualCost: math.NaN()}}})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	NewService(dashboardStubRepository{}).storeTrendCache(t.Context(), "nil-rdb", trend)
	if _, ok := NewService(dashboardStubRepository{}).loadTrendCache(t.Context(), "nil-rdb"); ok {
		t.Fatalf("nil redis cache should miss")
	}
}

func TestTrendLockHelpers(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	service := NewService(dashboardStubRepository{}, rdb)
	oldToken := newTrendLockToken
	newTrendLockToken = func() string { return "lock-token" }
	defer func() { newTrendLockToken = oldToken }()

	mock.ExpectSetNX("trend:lock", "lock-token", trendLockTTL).SetVal(true)
	token, ok, busy := service.tryLockTrendCache(t.Context(), "trend")
	if token != "lock-token" || !ok || busy {
		t.Fatalf("tryLock success = %q %v %v", token, ok, busy)
	}
	mock.ExpectSetNX("busy:lock", "lock-token", trendLockTTL).SetVal(false)
	if _, ok, busy := service.tryLockTrendCache(t.Context(), "busy"); ok || !busy {
		t.Fatalf("tryLock busy = %v %v, want busy", ok, busy)
	}
	mock.ExpectSetNX("error:lock", "lock-token", trendLockTTL).SetErr(errors.New("redis failed"))
	if _, ok, busy := service.tryLockTrendCache(t.Context(), "error"); ok || busy {
		t.Fatalf("tryLock error = %v %v, want false false", ok, busy)
	}
	mock.ExpectEvalSha(trendLockReleaseScript.Hash(), []string{"trend:lock"}, "lock-token").SetVal(int64(1))
	service.releaseTrendCacheLock(t.Context(), "trend", "lock-token")
	service.releaseTrendCacheLock(t.Context(), "trend", "")
	NewService(dashboardStubRepository{}).releaseTrendCacheLock(t.Context(), "trend", "lock-token")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	if token, ok, busy := NewService(dashboardStubRepository{}).tryLockTrendCache(t.Context(), "nil"); token != "" || ok || busy {
		t.Fatalf("nil redis tryLock = %q %v %v", token, ok, busy)
	}
}

func TestWaitForTrendCacheBranches(t *testing.T) {
	if _, ok := NewService(dashboardStubRepository{}).waitForTrendCache(t.Context(), "nil", time.Millisecond); ok {
		t.Fatalf("nil redis wait should miss")
	}

	rdb, mock := redismock.NewClientMock()
	service := NewService(dashboardStubRepository{}, rdb)
	trend := Trend{ModelDistribution: []ModelStats{{Model: "ready"}}}
	raw, err := json.Marshal(trend)
	if err != nil {
		t.Fatalf("marshal ready trend: %v", err)
	}
	mock.ExpectGet("ready").SetVal(string(raw))
	if got, ok := service.waitForTrendCache(t.Context(), "ready", time.Second); !ok || got.ModelDistribution[0].Model != "ready" {
		t.Fatalf("wait ready = %+v %v", got, ok)
	}
	mock.ExpectGet("timeout").RedisNil()
	if _, ok := service.waitForTrendCache(t.Context(), "timeout", 0); ok {
		t.Fatalf("wait timeout should miss")
	}
	mock.ExpectGet("tick").RedisNil()
	mock.ExpectGet("tick").RedisNil()
	if _, ok := service.waitForTrendCache(t.Context(), "tick", time.Millisecond); ok {
		t.Fatalf("wait tick timeout should miss")
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	mock.ExpectGet("canceled").RedisNil()
	if _, ok := service.waitForTrendCache(cancelCtx, "canceled", time.Hour); ok {
		t.Fatalf("wait canceled should miss")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestStopTrendWaitTimer(t *testing.T) {
	active := time.NewTimer(time.Hour)
	stopTrendWaitTimer(active)

	fired := time.NewTimer(time.Nanosecond)
	time.Sleep(time.Millisecond)
	stopTrendWaitTimer(fired)

	drained := time.NewTimer(time.Nanosecond)
	<-drained.C
	stopTrendWaitTimer(drained)
}

func TestAggregateTopUsersLimitsAndSortsDailyPoints(t *testing.T) {
	logs := make([]TrendLog, 0, 15)
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 13; i++ {
		logs = append(logs, TrendLog{
			UserID:       i,
			UserEmail:    "user@test.com",
			InputTokens:  int64(i),
			OutputTokens: int64(i),
			CreatedAt:    base.Add(time.Duration(i) * time.Hour),
		})
	}
	logs = append(logs,
		TrendLog{UserID: 13, UserEmail: "top@test.com", InputTokens: 100, CreatedAt: base.AddDate(0, 0, 1)},
		TrendLog{UserID: 13, UserEmail: "top@test.com", InputTokens: 100, CreatedAt: base},
	)
	result := aggregateTopUsers(logs, "day", time.UTC)
	if len(result) != 12 {
		t.Fatalf("top user count = %d, want 12", len(result))
	}
	if result[0].UserID != 13 || len(result[0].Trend) != 2 || result[0].Trend[0].Time > result[0].Trend[1].Time {
		t.Fatalf("top user trend = %+v", result[0])
	}
}

type dashboardStubRepository struct {
	loadStatsSnapshot func(context.Context, time.Time, time.Time) (StatsSnapshot, error)
	listTrendLogs     func(context.Context, time.Time, time.Time) ([]TrendLog, error)
}

func (s dashboardStubRepository) LoadStatsSnapshot(ctx context.Context, todayStart, fiveMinAgo time.Time, _ int) (StatsSnapshot, error) {
	if s.loadStatsSnapshot == nil {
		return StatsSnapshot{}, nil
	}
	return s.loadStatsSnapshot(ctx, todayStart, fiveMinAgo)
}

func (s dashboardStubRepository) ListTrendLogs(ctx context.Context, startTime, endTime time.Time, _ int) ([]TrendLog, error) {
	if s.listTrendLogs == nil {
		return nil, nil
	}
	return s.listTrendLogs(ctx, startTime, endTime)
}
