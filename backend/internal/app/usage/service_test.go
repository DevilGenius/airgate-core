package usage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestListUserAndAdminNormalizePaginationAndTotals(t *testing.T) {
	next := int64(99)
	repo := &stubUsageRepository{
		listUserFn: func(_ context.Context, userID int64, filter ListFilter) ([]LogRecord, bool, *int64, error) {
			if userID != 42 || filter.Page != 1 || filter.PageSize != 20 {
				t.Fatalf("ListUser args user=%d filter=%+v", userID, filter)
			}
			return []LogRecord{{ID: 1}, {ID: 2}}, true, &next, nil
		},
		listAdminFn: func(_ context.Context, filter ListFilter) ([]LogRecord, bool, *int64, error) {
			if filter.Page != 3 || filter.PageSize != 10 || filter.BeforeID != 50 {
				t.Fatalf("ListAdmin filter=%+v", filter)
			}
			return []LogRecord{{ID: 3}}, false, nil, nil
		},
	}
	svc := NewService(repo)

	userList, err := svc.ListUser(t.Context(), 42, ListFilter{Page: -1, PageSize: 0})
	if err != nil {
		t.Fatalf("ListUser() error = %v", err)
	}
	if userList.Total != 3 || !userList.HasMore || userList.NextCursor == nil || *userList.NextCursor != 99 || userList.TotalExact {
		t.Fatalf("ListUser() = %+v", userList)
	}
	adminList, err := svc.ListAdmin(t.Context(), ListFilter{Page: 3, PageSize: 10, BeforeID: 50})
	if err != nil {
		t.Fatalf("ListAdmin() error = %v", err)
	}
	if adminList.Total != 21 || adminList.HasMore || !adminList.TotalExact {
		t.Fatalf("ListAdmin() = %+v", adminList)
	}
}

func TestListErrorsAndStatsDelegates(t *testing.T) {
	repoErr := errors.New("repo failed")
	svc := NewService(&stubUsageRepository{
		listUserFn: func(context.Context, int64, ListFilter) ([]LogRecord, bool, *int64, error) {
			return nil, false, nil, repoErr
		},
		listAdminFn:    func(context.Context, ListFilter) ([]LogRecord, bool, *int64, error) { return nil, false, nil, repoErr },
		summaryUserFn:  func(context.Context, int64, StatsFilter) (Summary, error) { return Summary{}, repoErr },
		statsByModelFn: func(context.Context, StatsFilter) ([]ModelStats, error) { return nil, repoErr },
	})

	if _, err := svc.ListUser(t.Context(), 1, ListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("ListUser error = %v", err)
	}
	if _, err := svc.ListAdmin(t.Context(), ListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("ListAdmin error = %v", err)
	}
	if _, err := svc.UserStats(t.Context(), 1, StatsFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("UserStats error = %v", err)
	}
	if _, err := svc.StatsByModel(t.Context(), StatsFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("StatsByModel error = %v", err)
	}
}

func TestUserStatsWithModelsCombinesSummaryAndModelStats(t *testing.T) {
	repo := &stubUsageRepository{
		summaryUserFn: func(_ context.Context, userID int64, filter StatsFilter) (Summary, error) {
			if userID != 42 {
				t.Fatalf("SummaryUser userID = %d, want 42", userID)
			}
			if filter.Platform != "openai" || filter.Model != "gpt-5.5" {
				t.Fatalf("SummaryUser filter = %+v, want openai/gpt-5.5", filter)
			}
			return Summary{TotalRequests: 7, TotalTokens: 99, TotalBilledCost: 3.14}, nil
		},
		statsByModelFn: func(_ context.Context, filter StatsFilter) ([]ModelStats, error) {
			if filter.UserID == nil || *filter.UserID != 42 {
				t.Fatalf("StatsByModel userID = %v, want 42", filter.UserID)
			}
			return []ModelStats{{Model: "gpt-5.5", Requests: 2, Tokens: 11, BilledCost: 1.23}}, nil
		},
	}

	svc := NewService(repo)
	result, err := svc.UserStatsWithModels(context.Background(), 42, StatsFilter{
		Platform: "openai",
		Model:    "gpt-5.5",
	})
	if err != nil {
		t.Fatalf("UserStatsWithModels returned error: %v", err)
	}
	if got, want := result.Summary.TotalRequests, int64(7); got != want {
		t.Fatalf("Summary.TotalRequests = %d, want %d", got, want)
	}
	if got, want := len(result.ByModel), 1; got != want {
		t.Fatalf("len(ByModel) = %d, want %d", got, want)
	}
	if got, want := result.ByModel[0].Model, "gpt-5.5"; got != want {
		t.Fatalf("ByModel[0].Model = %q, want %q", got, want)
	}
}

func TestUserStatsWithModelsPropagatesModelStatsError(t *testing.T) {
	repoErr := errors.New("model stats failed")
	repo := &stubUsageRepository{
		summaryUserFn:  func(context.Context, int64, StatsFilter) (Summary, error) { return Summary{TotalRequests: 1}, nil },
		statsByModelFn: func(context.Context, StatsFilter) ([]ModelStats, error) { return nil, repoErr },
	}

	_, err := NewService(repo).UserStatsWithModels(t.Context(), 1, StatsFilter{})
	if !errors.Is(err, repoErr) {
		t.Fatalf("UserStatsWithModels() error = %v", err)
	}
}

func TestAdminStatsBuildsRequestedDimensions(t *testing.T) {
	called := make(map[string]bool)
	repo := &stubUsageRepository{
		summaryAdminFn: func(context.Context, StatsFilter) (Summary, error) {
			called["summary"] = true
			return Summary{TotalRequests: 10}, nil
		},
		statsByModelFn: func(context.Context, StatsFilter) ([]ModelStats, error) {
			called["model"] = true
			return []ModelStats{{Model: "gpt-4.1", Requests: 2}}, nil
		},
		statsByUserFn: func(context.Context, StatsFilter) ([]UserStats, error) {
			called["user"] = true
			return []UserStats{{UserID: 1, Requests: 3}}, nil
		},
		statsByAccountFn: func(context.Context, StatsFilter) ([]AccountStats, error) {
			called["account"] = true
			return []AccountStats{{AccountID: 2, Requests: 4}}, nil
		},
		statsByGroupFn: func(context.Context, StatsFilter) ([]GroupStats, error) {
			called["group"] = true
			return []GroupStats{{GroupID: 3, Requests: 5}}, nil
		},
	}

	result, err := NewService(repo).AdminStats(t.Context(), StatsFilter{Platform: "openai"}, "user,model,account,group,unknown,user")
	if err != nil {
		t.Fatalf("AdminStats() error = %v", err)
	}
	for _, key := range []string{"summary", "model", "user", "account", "group"} {
		if !called[key] {
			t.Fatalf("%s stats were not called", key)
		}
	}
	if result.TotalRequests != 10 || len(result.ByModel) != 1 || len(result.ByUser) != 1 || len(result.ByAccount) != 1 || len(result.ByGroup) != 1 {
		t.Fatalf("AdminStats() = %+v", result)
	}
}

func TestAdminStatsPropagatesDimensionError(t *testing.T) {
	repoErr := errors.New("group failed")
	repo := &stubUsageRepository{
		summaryAdminFn: func(context.Context, StatsFilter) (Summary, error) { return Summary{}, nil },
		statsByGroupFn: func(context.Context, StatsFilter) ([]GroupStats, error) { return nil, repoErr },
	}

	_, err := NewService(repo).AdminStats(t.Context(), StatsFilter{}, "group")
	if !errors.Is(err, repoErr) {
		t.Fatalf("AdminStats() error = %v", err)
	}
}

func TestAdminTrendBuildsBuckets(t *testing.T) {
	repo := &stubUsageRepository{
		trendEntriesFn: func(_ context.Context, filter TrendFilter) ([]TrendEntry, error) {
			if filter.DefaultRecentHours != 24 || filter.Granularity != "hour" {
				t.Fatalf("Trend filter = %+v", filter)
			}
			return []TrendEntry{
				{CreatedAt: "2026-06-20T01:15:00Z", InputTokens: 1, OutputTokens: 2, ActualCost: 0.1},
				{CreatedAt: "2026-06-20T01:45:00Z", InputTokens: 3, OutputTokens: 4, ActualCost: 0.2},
			}, nil
		},
	}

	buckets, err := NewService(repo).AdminTrend(t.Context(), TrendFilter{Granularity: "hour", StatsFilter: StatsFilter{TZ: "UTC"}})
	if err != nil {
		t.Fatalf("AdminTrend() error = %v", err)
	}
	if len(buckets) != 1 || buckets[0].InputTokens != 4 || buckets[0].OutputTokens != 6 {
		t.Fatalf("AdminTrend() = %+v", buckets)
	}
}

func TestAdminTrendPropagatesError(t *testing.T) {
	repoErr := errors.New("trend failed")
	repo := &stubUsageRepository{
		trendEntriesFn: func(context.Context, TrendFilter) ([]TrendEntry, error) { return nil, repoErr },
	}

	_, err := NewService(repo).AdminTrend(t.Context(), TrendFilter{})
	if !errors.Is(err, repoErr) {
		t.Fatalf("AdminTrend() error = %v", err)
	}
}

func TestNormalizeStatsGroupByDropsUnknownAndDuplicates(t *testing.T) {
	got := normalizeStatsGroupBy("user, model,foo,group,user,account,model")
	want := "account,group,model,user"
	if got != want {
		t.Fatalf("normalizeStatsGroupBy() = %q, want %q", got, want)
	}
}

func TestNormalizeTrendFilterDefaultsRecentHours(t *testing.T) {
	got := normalizeTrendFilter(TrendFilter{})
	if got.DefaultRecentHours != 24 {
		t.Fatalf("DefaultRecentHours = %d, want 24", got.DefaultRecentHours)
	}
}

func TestUsageListTotalAndCacheHelpersWithoutRedis(t *testing.T) {
	if got := usageListTotal(0, 0, 2, true); got != 3 {
		t.Fatalf("usageListTotal default = %d, want 3", got)
	}
	if got := usageListTotal(3, 10, 1, false); got != 21 {
		t.Fatalf("usageListTotal page 3 = %d, want 21", got)
	}

	key := usageCacheKey("admin-stats", map[string]string{"b": "2", "a": "1"})
	if !strings.HasPrefix(key, usageCacheKeyPrefix+":admin:stats:") {
		t.Fatalf("usageCacheKey() = %q", key)
	}

	loaded, err := usageCachedResult[int](t.Context(), nil, "key", time.Second, func(context.Context) (int, error) {
		return 7, nil
	})
	if err != nil || loaded != 7 {
		t.Fatalf("usageCachedResult nil redis = %d/%v", loaded, err)
	}
	repoErr := errors.New("loader failed")
	if _, err := usageCachedResult[int](t.Context(), nil, "key", time.Second, func(context.Context) (int, error) {
		return 0, repoErr
	}); !errors.Is(err, repoErr) {
		t.Fatalf("usageCachedResult error = %v", err)
	}
}

func TestNormalizePage(t *testing.T) {
	if page, pageSize := NormalizePage(0, -1); page != 1 || pageSize != 20 {
		t.Fatalf("NormalizePage default = %d/%d", page, pageSize)
	}
	if page, pageSize := NormalizePage(2, 50); page != 2 || pageSize != 50 {
		t.Fatalf("NormalizePage provided = %d/%d", page, pageSize)
	}
}

type stubUsageRepository struct {
	listUserFn       func(context.Context, int64, ListFilter) ([]LogRecord, bool, *int64, error)
	listAdminFn      func(context.Context, ListFilter) ([]LogRecord, bool, *int64, error)
	summaryUserFn    func(context.Context, int64, StatsFilter) (Summary, error)
	summaryAdminFn   func(context.Context, StatsFilter) (Summary, error)
	statsByModelFn   func(context.Context, StatsFilter) ([]ModelStats, error)
	statsByUserFn    func(context.Context, StatsFilter) ([]UserStats, error)
	statsByAccountFn func(context.Context, StatsFilter) ([]AccountStats, error)
	statsByGroupFn   func(context.Context, StatsFilter) ([]GroupStats, error)
	trendEntriesFn   func(context.Context, TrendFilter) ([]TrendEntry, error)
}

func (s *stubUsageRepository) ListUser(ctx context.Context, userID int64, filter ListFilter) ([]LogRecord, bool, *int64, error) {
	if s.listUserFn != nil {
		return s.listUserFn(ctx, userID, filter)
	}
	return nil, false, nil, nil
}

func (s *stubUsageRepository) ListAdmin(ctx context.Context, filter ListFilter) ([]LogRecord, bool, *int64, error) {
	if s.listAdminFn != nil {
		return s.listAdminFn(ctx, filter)
	}
	return nil, false, nil, nil
}

func (s *stubUsageRepository) SummaryUser(ctx context.Context, userID int64, filter StatsFilter) (Summary, error) {
	if s.summaryUserFn != nil {
		return s.summaryUserFn(ctx, userID, filter)
	}
	return Summary{}, nil
}

func (s *stubUsageRepository) SummaryAdmin(ctx context.Context, filter StatsFilter) (Summary, error) {
	if s.summaryAdminFn != nil {
		return s.summaryAdminFn(ctx, filter)
	}
	return Summary{}, nil
}

func (s *stubUsageRepository) StatsByModel(ctx context.Context, filter StatsFilter) ([]ModelStats, error) {
	if s.statsByModelFn != nil {
		return s.statsByModelFn(ctx, filter)
	}
	return nil, nil
}

func (s *stubUsageRepository) StatsByUser(ctx context.Context, filter StatsFilter) ([]UserStats, error) {
	if s.statsByUserFn != nil {
		return s.statsByUserFn(ctx, filter)
	}
	return nil, nil
}

func (s *stubUsageRepository) StatsByAccount(ctx context.Context, filter StatsFilter) ([]AccountStats, error) {
	if s.statsByAccountFn != nil {
		return s.statsByAccountFn(ctx, filter)
	}
	return nil, nil
}

func (s *stubUsageRepository) StatsByGroup(ctx context.Context, filter StatsFilter) ([]GroupStats, error) {
	if s.statsByGroupFn != nil {
		return s.statsByGroupFn(ctx, filter)
	}
	return nil, nil
}

func (s *stubUsageRepository) TrendEntries(ctx context.Context, filter TrendFilter) ([]TrendEntry, error) {
	if s.trendEntriesFn != nil {
		return s.trendEntriesFn(ctx, filter)
	}
	return nil, nil
}
