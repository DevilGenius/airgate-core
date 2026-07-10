package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
)

type usageRouteRepoStub struct {
	err error

	lastUserID         int64
	lastUserListFilter appusage.ListFilter
	lastAdminFilter    appusage.ListFilter
	lastStatsFilter    appusage.StatsFilter
	lastTrendFilter    appusage.TrendFilter
}

func (r *usageRouteRepoStub) ListUser(_ context.Context, userID int64, filter appusage.ListFilter) ([]appusage.LogRecord, bool, *int64, error) {
	r.lastUserID = userID
	r.lastUserListFilter = filter
	if r.err != nil {
		return nil, false, nil, r.err
	}
	next := int64(41)
	return []appusage.LogRecord{usageRouteLogRecord()}, true, &next, nil
}

func (r *usageRouteRepoStub) ListAdmin(_ context.Context, filter appusage.ListFilter) ([]appusage.LogRecord, bool, *int64, error) {
	r.lastAdminFilter = filter
	if r.err != nil {
		return nil, false, nil, r.err
	}
	next := int64(51)
	return []appusage.LogRecord{usageRouteLogRecord()}, true, &next, nil
}

func (r *usageRouteRepoStub) SummaryUser(_ context.Context, userID int64, filter appusage.StatsFilter) (appusage.Summary, error) {
	r.lastUserID = userID
	r.lastStatsFilter = filter
	if r.err != nil {
		return appusage.Summary{}, r.err
	}
	return usageRouteSummary(), nil
}

func (r *usageRouteRepoStub) SummaryAdmin(_ context.Context, filter appusage.StatsFilter) (appusage.Summary, error) {
	r.lastStatsFilter = filter
	if r.err != nil {
		return appusage.Summary{}, r.err
	}
	return usageRouteSummary(), nil
}

func (r *usageRouteRepoStub) StatsByModel(_ context.Context, filter appusage.StatsFilter) ([]appusage.ModelStats, error) {
	r.lastStatsFilter = filter
	if r.err != nil {
		return nil, r.err
	}
	return []appusage.ModelStats{{Model: "gpt-4.1", Requests: 3, Tokens: 30, TotalCost: 1.25, ActualCost: 1.1, BilledCost: 1.8}}, nil
}

func (r *usageRouteRepoStub) StatsByUser(context.Context, appusage.StatsFilter) ([]appusage.UserStats, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []appusage.UserStats{{UserID: 7, Email: "user@example.test", Requests: 2, Tokens: 20, TotalCost: 0.9, ActualCost: 0.8, BilledCost: 1.2}}, nil
}

func (r *usageRouteRepoStub) StatsByAccount(context.Context, appusage.StatsFilter) ([]appusage.AccountStats, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []appusage.AccountStats{{AccountID: 9, Name: "acct", Requests: 1, Tokens: 10, TotalCost: 0.4, ActualCost: 0.3, BilledCost: 0.5}}, nil
}

func (r *usageRouteRepoStub) StatsByGroup(context.Context, appusage.StatsFilter) ([]appusage.GroupStats, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []appusage.GroupStats{{GroupID: 11, Name: "group", Requests: 1, Tokens: 10, TotalCost: 0.4, ActualCost: 0.3, BilledCost: 0.5}}, nil
}

func (r *usageRouteRepoStub) TrendEntries(_ context.Context, filter appusage.TrendFilter) ([]appusage.TrendEntry, error) {
	r.lastTrendFilter = filter
	if r.err != nil {
		return nil, r.err
	}
	return []appusage.TrendEntry{{
		CreatedAt:           "2026-06-20T10:15:00Z",
		InputTokens:         10,
		OutputTokens:        5,
		CachedInputTokens:   3,
		CacheCreationTokens: 2,
		ActualCost:          0.7,
		StandardCost:        0.8,
		BilledCost:          1.1,
	}}, nil
}

func usageRouteLogRecord() appusage.LogRecord {
	return appusage.LogRecord{
		ID:                    31,
		UserID:                7,
		UserEmail:             "user@example.test",
		APIKeyID:              13,
		APIKeyName:            "key",
		APIKeyHint:            "sk-...test",
		AccountID:             17,
		AccountName:           "acct",
		AccountEmail:          "acct@example.test",
		GroupID:               19,
		Platform:              "openai",
		Model:                 "gpt-4.1",
		InputTokens:           10,
		OutputTokens:          5,
		CachedInputTokens:     3,
		CacheCreationTokens:   2,
		ReasoningOutputTokens: 1,
		InputPrice:            0.1,
		OutputPrice:           0.2,
		CachedInputPrice:      0.03,
		CacheCreationPrice:    0.04,
		InputCost:             0.5,
		OutputCost:            0.25,
		CachedInputCost:       0.03,
		CacheCreationCost:     0.04,
		TotalCost:             0.82,
		ActualCost:            0.8,
		BilledCost:            1.2,
		AccountCost:           0.6,
		RateMultiplier:        1.25,
		SellRate:              1.5,
		AccountRateMultiplier: 1.3,
		ServiceTier:           "priority",
		Stream:                true,
		DurationMs:            1200,
		FirstTokenMs:          120,
		UserAgent:             "agent",
		IPAddress:             "127.0.0.1",
		Endpoint:              "/v1/messages",
		ReasoningEffort:       "medium",
		UsageMetadata:         map[string]string{"request_id": "req-1"},
		CreatedAt:             "2026-06-20T10:15:00Z",
	}
}

func usageRouteSummary() appusage.Summary {
	return appusage.Summary{TotalRequests: 3, TotalTokens: 30, TotalCost: 1.25, TotalActualCost: 1.1, TotalBilledCost: 1.8}
}

func TestUsageRoutesReturnScopedAndAdminPayloads(t *testing.T) {
	repo := &usageRouteRepoStub{}
	handler := NewUsageHandler(appusage.NewService(repo))

	w := invokeHandlerForValidation(http.MethodGet, "/usage?page=2&page_size=5&api_key_id=1&before_id=99&tz=UTC", "", nil, func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set(middleware.CtxKeyAPIKeyID, 42)
	}, handler.UserUsage)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if repo.lastUserID != 7 || repo.lastUserListFilter.APIKeyID == nil || *repo.lastUserListFilter.APIKeyID != 42 || !repo.lastUserListFilter.ScopedToKey {
		t.Fatalf("scoped list filter = %+v user=%d", repo.lastUserListFilter, repo.lastUserID)
	}
	if !strings.Contains(w.Body.String(), `"cost":1.2`) || !strings.Contains(w.Body.String(), `"effective_rate":1.875`) ||
		!strings.Contains(w.Body.String(), `"input_price":0.1`) || !strings.Contains(w.Body.String(), `"cached_input_price":0.03`) ||
		!strings.Contains(w.Body.String(), `"input_cost":0.5`) || !strings.Contains(w.Body.String(), `"total_cost":0.82`) ||
		strings.Contains(w.Body.String(), `"rate_multiplier"`) || strings.Contains(w.Body.String(), `"sell_rate"`) ||
		strings.Contains(w.Body.String(), `"actual_cost"`) || strings.Contains(w.Body.String(), `"account_cost"`) {
		t.Fatalf("scoped usage body leaked reseller/account fields: %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/usage/stats?api_key_id=1&tz=UTC", "", nil, func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set(middleware.CtxKeyAPIKeyID, 42)
	}, handler.UserUsageStats)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if repo.lastStatsFilter.APIKeyID == nil || *repo.lastStatsFilter.APIKeyID != 42 || !repo.lastStatsFilter.ScopedToKey {
		t.Fatalf("scoped stats filter = %+v", repo.lastStatsFilter)
	}
	if !strings.Contains(w.Body.String(), `"total_billed_cost":1.8`) || !strings.Contains(w.Body.String(), `"billed_cost":1.8`) {
		t.Fatalf("scoped stats body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/usage/trend?granularity=hour&api_key_id=1&tz=UTC", "", nil, func(c *gin.Context) {
		c.Set("user_id", 7)
		c.Set(middleware.CtxKeyAPIKeyID, 42)
	}, handler.UserUsageTrend)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if repo.lastTrendFilter.APIKeyID == nil || *repo.lastTrendFilter.APIKeyID != 42 || !repo.lastTrendFilter.ScopedToKey {
		t.Fatalf("scoped trend filter = %+v", repo.lastTrendFilter)
	}
	if !strings.Contains(w.Body.String(), `"billed_cost":1.1`) || !strings.Contains(w.Body.String(), `"time":"2026-06-20 10:00"`) {
		t.Fatalf("scoped trend body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/admin/usage?page=1&page_size=5&before_id=80&user_id=7&api_key_id=13&account_id=17&group_id=19&platform=openai&model=gpt-4.1&tz=UTC", "", nil, nil, handler.AdminUsage)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if repo.lastAdminFilter.UserID == nil || *repo.lastAdminFilter.UserID != 7 || repo.lastAdminFilter.BeforeID != 80 {
		t.Fatalf("admin list filter = %+v", repo.lastAdminFilter)
	}
	if !strings.Contains(w.Body.String(), `"account_cost":0.6`) || !strings.Contains(w.Body.String(), `"account_rate_multiplier":1.3`) {
		t.Fatalf("admin usage body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/admin/usage/stats?group_by=group,account,model,user&user_id=7&api_key_id=13&platform=openai&model=gpt-4.1&tz=UTC", "", nil, nil, handler.AdminUsageStats)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	for _, want := range []string{`"by_model"`, `"by_user"`, `"by_account"`, `"by_group"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("admin stats body missing %s: %s", want, w.Body.String())
		}
	}

	w = invokeHandlerForValidation(http.MethodGet, "/admin/usage/trend?granularity=day&user_id=7&api_key_id=13&platform=openai&model=gpt-4.1&tz=UTC", "", nil, nil, handler.AdminUsageTrend)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if repo.lastTrendFilter.UserID == nil || *repo.lastTrendFilter.UserID != 7 || repo.lastTrendFilter.Granularity != "day" {
		t.Fatalf("admin trend filter = %+v", repo.lastTrendFilter)
	}
	if !strings.Contains(w.Body.String(), `"actual_cost":0.7`) || strings.Contains(w.Body.String(), `"billed_cost"`) {
		t.Fatalf("admin trend body = %s", w.Body.String())
	}
}

func TestUsageRoutesMapServiceErrors(t *testing.T) {
	handler := NewUsageHandler(appusage.NewService(&usageRouteRepoStub{err: errors.New("repo down")}))
	withUser := func(c *gin.Context) { c.Set("user_id", 7) }

	tests := []struct {
		name   string
		target string
		setup  func(*gin.Context)
		fn     func(*gin.Context)
	}{
		{name: "user usage", target: "/usage?page=1&page_size=10", setup: withUser, fn: handler.UserUsage},
		{name: "user stats", target: "/usage/stats", setup: withUser, fn: handler.UserUsageStats},
		{name: "user trend", target: "/usage/trend?granularity=day", setup: withUser, fn: handler.UserUsageTrend},
		{name: "admin usage", target: "/admin/usage?page=1&page_size=10", fn: handler.AdminUsage},
		{name: "admin stats", target: "/admin/usage/stats?group_by=model", fn: handler.AdminUsageStats},
		{name: "admin trend", target: "/admin/usage/trend?granularity=day", fn: handler.AdminUsageTrend},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(http.MethodGet, tt.target, "", nil, tt.setup, tt.fn)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
			}
		})
	}
}
