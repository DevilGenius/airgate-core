package store

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
)

func TestUsageStoreListPaginationUsesStableIDOrder(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "usage-pagination@example.com")
	sameCreatedAt := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		if _, err := db.UsageLog.Create().
			SetBillingEventID("bill_usage_pagination_" + string(rune('a'+i))).
			SetPlatform("openai").
			SetModel("gpt-5").
			SetUserID(user.ID).
			SetUserIDSnapshot(user.ID).
			SetUserEmailSnapshot(user.Email).
			SetCreatedAt(sameCreatedAt).
			Save(ctx); err != nil {
			t.Fatalf("create usage log: %v", err)
		}
	}

	store := NewUsageStore(db)

	t.Run("admin list", func(t *testing.T) {
		page1, hasMore, nextCursor, err := store.ListAdmin(ctx, appusage.ListFilter{Page: 1, PageSize: 2})
		if err != nil {
			t.Fatalf("ListAdmin page 1 returned error: %v", err)
		}
		if !hasMore {
			t.Fatalf("ListAdmin page 1 hasMore = false, want true")
		}
		if nextCursor == nil || *nextCursor != 2 {
			t.Fatalf("ListAdmin page 1 nextCursor = %v, want 2", nextCursor)
		}
		assertLogIDs(t, page1, 3, 2)

		page2, hasMore, nextCursor, err := store.ListAdmin(ctx, appusage.ListFilter{Page: 2, PageSize: 2, BeforeID: *nextCursor})
		if err != nil {
			t.Fatalf("ListAdmin page 2 returned error: %v", err)
		}
		if hasMore {
			t.Fatalf("ListAdmin page 2 hasMore = true, want false")
		}
		if nextCursor != nil {
			t.Fatalf("ListAdmin page 2 nextCursor = %v, want nil", *nextCursor)
		}
		assertLogIDs(t, page2, 1)
	})

	t.Run("user list", func(t *testing.T) {
		page1, hasMore, nextCursor, err := store.ListUser(ctx, int64(user.ID), appusage.ListFilter{Page: 1, PageSize: 2})
		if err != nil {
			t.Fatalf("ListUser page 1 returned error: %v", err)
		}
		if !hasMore {
			t.Fatalf("ListUser page 1 hasMore = false, want true")
		}
		if nextCursor == nil || *nextCursor != 2 {
			t.Fatalf("ListUser page 1 nextCursor = %v, want 2", nextCursor)
		}
		assertLogIDs(t, page1, 3, 2)

		page2, hasMore, nextCursor, err := store.ListUser(ctx, int64(user.ID), appusage.ListFilter{Page: 2, PageSize: 2, BeforeID: *nextCursor})
		if err != nil {
			t.Fatalf("ListUser page 2 returned error: %v", err)
		}
		if hasMore {
			t.Fatalf("ListUser page 2 hasMore = true, want false")
		}
		if nextCursor != nil {
			t.Fatalf("ListUser page 2 nextCursor = %v, want nil", *nextCursor)
		}
		assertLogIDs(t, page2, 1)
	})
}

func assertLogIDs(t *testing.T, got []appusage.LogRecord, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d; got %+v", len(got), len(want), got)
	}
	for i, item := range got {
		if item.ID != want[i] {
			t.Fatalf("got[%d].ID = %d, want %d; got %+v", i, item.ID, want[i], got)
		}
	}
}

func TestParseUsageModelFilter(t *testing.T) {
	tests := []struct {
		raw          string
		wantIncludes []string
		wantExcludes []string
	}{
		{raw: ""},
		{raw: "   "},
		{raw: "!"},
		{raw: " !  "},
		{raw: "gpt-5.4-mini", wantIncludes: []string{"gpt-5.4-mini"}},
		{raw: " gpt-5.4-mini ", wantIncludes: []string{"gpt-5.4-mini"}},
		{raw: "gpt-5.4 gpt-5.5", wantIncludes: []string{"gpt-5.4", "gpt-5.5"}},
		{raw: "gpt-5.4\t!gpt-5.4-mini\n!gpt-5.5-mini", wantIncludes: []string{"gpt-5.4"}, wantExcludes: []string{"gpt-5.4-mini", "gpt-5.5-mini"}},
		{raw: "!gpt-5.4-mini", wantExcludes: []string{"gpt-5.4-mini"}},
		{raw: " ! gpt-5.4-mini ", wantExcludes: []string{"gpt-5.4-mini"}},
		{raw: "gpt!mini", wantIncludes: []string{"gpt!mini"}},
		{raw: "!!foo", wantExcludes: []string{"!foo"}},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			includes, excludes := parseUsageModelFilter(tt.raw)
			if !slices.Equal(includes, tt.wantIncludes) || !slices.Equal(excludes, tt.wantExcludes) {
				t.Fatalf("parseUsageModelFilter(%q) = includes %q, excludes %q; want includes %q, excludes %q", tt.raw, includes, excludes, tt.wantIncludes, tt.wantExcludes)
			}
		})
	}
}

func TestUsageStoreModelFilterIncludeExclude(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "usage-model-filter@example.com")
	models := []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.4-mini-fast", "gpt-5.5", "gpt-5.5-mini", "claude-sonnet-4"}
	for i, model := range models {
		if _, err := db.UsageLog.Create().
			SetBillingEventID("bill_usage_model_filter_" + string(rune('a'+i))).
			SetPlatform("openai").
			SetModel(model).
			SetInputTokens(1).
			SetTotalCost(1).
			SetActualCost(1).
			SetBilledCost(1).
			SetUserID(user.ID).
			SetUserIDSnapshot(user.ID).
			SetUserEmailSnapshot(user.Email).
			SetCreatedAt(time.Date(2026, 7, 12, i, 0, 0, 0, time.UTC)).
			Save(ctx); err != nil {
			t.Fatalf("create usage log for %s: %v", model, err)
		}
	}

	store := NewUsageStore(db)
	include, _, _, err := store.ListUser(ctx, int64(user.ID), appusage.ListFilter{PageSize: 10, Model: "gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("ListUser include returned error: %v", err)
	}
	if len(include) != 2 {
		t.Fatalf("include records = %+v, want 2 mini models", include)
	}

	multiInclude, _, _, err := store.ListAdmin(ctx, appusage.ListFilter{PageSize: 10, Model: "gpt-5.4 gpt-5.5"})
	if err != nil {
		t.Fatalf("ListAdmin multiple includes returned error: %v", err)
	}
	if len(multiInclude) != 5 {
		t.Fatalf("multiple include records = %+v, want 5 GPT models", multiInclude)
	}

	combined, _, _, err := store.ListUser(ctx, int64(user.ID), appusage.ListFilter{PageSize: 10, Model: "gpt-5.4 !gpt-5.4-mini"})
	if err != nil {
		t.Fatalf("ListUser combined filter returned error: %v", err)
	}
	if len(combined) != 1 || combined[0].Model != "gpt-5.4" {
		t.Fatalf("combined filter records = %+v, want only gpt-5.4", combined)
	}

	excludeFilter := " ! gpt-5.4-mini "
	excluded, _, _, err := store.ListAdmin(ctx, appusage.ListFilter{PageSize: 10, Model: excludeFilter})
	if err != nil {
		t.Fatalf("ListAdmin exclude returned error: %v", err)
	}
	if len(excluded) != 4 {
		t.Fatalf("exclude records = %+v, want 4 models not containing gpt-5.4-mini", excluded)
	}
	for _, record := range excluded {
		if strings.Contains(record.Model, "gpt-5.4-mini") {
			t.Fatalf("excluded model still present: %s", record.Model)
		}
	}

	statsFilter := appusage.StatsFilter{UserID: storePtr(int64(user.ID)), Model: excludeFilter}
	summary, err := store.SummaryAdmin(ctx, statsFilter)
	if err != nil {
		t.Fatalf("SummaryAdmin exclude returned error: %v", err)
	}
	if summary.TotalRequests != 4 || summary.TotalTokens != 4 || summary.TotalCost != 4 {
		t.Fatalf("exclude summary = %+v, want 4 records/tokens/cost", summary)
	}
	byModel, err := store.StatsByModel(ctx, statsFilter)
	if err != nil {
		t.Fatalf("StatsByModel exclude returned error: %v", err)
	}
	if len(byModel) != 4 {
		t.Fatalf("exclude model stats = %+v, want 4", byModel)
	}
	trend, err := store.TrendEntries(ctx, appusage.TrendFilter{StatsFilter: statsFilter})
	if err != nil {
		t.Fatalf("TrendEntries exclude returned error: %v", err)
	}
	if len(trend) != 4 {
		t.Fatalf("exclude trend = %+v, want 4", trend)
	}

	all, _, _, err := store.ListUser(ctx, int64(user.ID), appusage.ListFilter{PageSize: 10, Model: "!"})
	if err != nil {
		t.Fatalf("ListUser bare ! returned error: %v", err)
	}
	if len(all) != len(models) {
		t.Fatalf("bare ! records = %d, want %d", len(all), len(models))
	}
}

func TestUsageStoreEmptySummaryReturnsZero(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	summary, err := NewUsageStore(db).SummaryAdmin(t.Context(), appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("SummaryAdmin empty returned error: %v", err)
	}
	if summary != (appusage.Summary{}) {
		t.Fatalf("SummaryAdmin empty = %+v, want zero summary", summary)
	}
}
