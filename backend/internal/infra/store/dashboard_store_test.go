package store

import (
	"context"
	"testing"
	"time"

	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/accountusage"
	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
)

func TestDashboardStoreLoadStatsSnapshotAggregatesUsageLogsInSQL(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	todayStart := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	oneMinAgo := time.Date(2026, 5, 27, 11, 57, 0, 0, time.UTC)
	tenMinAgo := time.Date(2026, 5, 27, 11, 50, 0, 0, time.UTC)

	u, err := db.User.Create().
		SetEmail("active@example.com").
		SetPasswordHash("secret").
		SetCreatedAt(todayStart.Add(30 * time.Minute)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	createAccount := func(name string, state entaccount.State, errorMsg string) {
		t.Helper()
		builder := db.Account.Create().
			SetName(name).
			SetPlatform("openai").
			SetType("apikey").
			SetCredentials(map[string]string{"api_key": name}).
			SetState(state)
		if errorMsg != "" {
			builder = builder.SetErrorMsg(errorMsg)
		}
		if _, err := builder.Save(ctx); err != nil {
			t.Fatalf("create account %q: %v", name, err)
		}
	}
	createAccount("active", entaccount.StateActive, "")
	createAccount("limited", entaccount.StateRateLimited, "")
	createAccount("degraded", entaccount.StateDegraded, "")
	createAccount("closed-empty", entaccount.StateDisabled, "")
	createAccount("closed-manual", entaccount.StateDisabled, accountManualClosedReason)
	createAccount("error", entaccount.StateDisabled, "invalid credentials")
	if _, err := db.Account.Create().
		SetName("soft-deleted").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "soft-deleted"}).
		SetState(entaccount.StateActive).
		SetDeletedAt(todayStart.Add(time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create soft-deleted account: %v", err)
	}

	if _, err := db.UsageLog.Create().
		SetBillingEventID("bill_dashboard_relation_stats").
		SetPlatform("openai").
		SetModel("gpt-4.1").
		SetInputTokens(10).
		SetOutputTokens(20).
		SetCachedInputTokens(3).
		SetCacheCreationTokens(4).
		SetActualCost(1.5).
		SetTotalCost(2.0).
		SetDurationMs(1200).
		SetFirstEventMs(300).
		SetFirstTokenMs(450).
		SetUser(u).
		SetCreatedAt(todayStart.Add(2 * time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create relation usage log: %v", err)
	}

	if _, err := db.UsageLog.Create().
		SetBillingEventID("bill_dashboard_snapshot_stats").
		SetPlatform("openai").
		SetModel("gpt-image-1").
		SetInputTokens(1).
		SetOutputTokens(2).
		SetActualCost(3.5).
		SetTotalCost(5.0).
		SetAccountCost(4.0).
		SetDurationMs(2400).
		SetUserIDSnapshot(u.ID).
		SetUserEmailSnapshot(u.Email).
		SetCreatedAt(time.Date(2026, 5, 27, 11, 56, 0, 0, time.UTC)).
		Save(ctx); err != nil {
		t.Fatalf("create snapshot usage log: %v", err)
	}

	store := NewDashboardStore(db)
	snapshot, err := store.LoadStatsSnapshot(ctx, todayStart, oneMinAgo, tenMinAgo, u.ID)
	if err != nil {
		t.Fatalf("LoadStatsSnapshot returned error: %v", err)
	}

	if snapshot.TotalUsers != 1 || snapshot.NewUsersToday != 1 {
		t.Fatalf("user counts = (%d, %d), want (1, 1)", snapshot.TotalUsers, snapshot.NewUsersToday)
	}
	if snapshot.TotalAccounts != 6 || snapshot.EnabledAccounts != 3 || snapshot.ClosedAccounts != 2 || snapshot.ErrorAccounts != 1 {
		t.Fatalf("account counts = (%d, %d, %d, %d), want (6, 3, 2, 1)", snapshot.TotalAccounts, snapshot.EnabledAccounts, snapshot.ClosedAccounts, snapshot.ErrorAccounts)
	}
	if snapshot.TodayRequests != 2 || snapshot.TodayImageRequests != 1 || snapshot.TodayNonImageRequests != 1 {
		t.Fatalf("today request counts = (%d, %d, %d), want (2, 1, 1)", snapshot.TodayRequests, snapshot.TodayImageRequests, snapshot.TodayNonImageRequests)
	}
	if snapshot.TodayTokens != 40 || snapshot.AllTimeTokens != 40 || snapshot.RecentTokens1M != 0 || snapshot.RecentTokens10M != 3 {
		t.Fatalf("token counts = (%d, %d, %d, %d), want (40, 40, 0, 3)", snapshot.TodayTokens, snapshot.AllTimeTokens, snapshot.RecentTokens1M, snapshot.RecentTokens10M)
	}
	if snapshot.TodayCost != 5.0 || snapshot.TodayStandardCost != 7.0 {
		t.Fatalf("today costs = (%v, %v), want (5.0, 7.0)", snapshot.TodayCost, snapshot.TodayStandardCost)
	}
	if snapshot.AllTimeCost != 5.0 || snapshot.AllTimeStandardCost != 7.0 {
		t.Fatalf("all-time costs = (%v, %v), want (5.0, 7.0)", snapshot.AllTimeCost, snapshot.AllTimeStandardCost)
	}
	if snapshot.TodayNonImageDurationMs != 1200 || snapshot.TodayFirstEventRequests != 1 || snapshot.TodayFirstEventMs != 300 || snapshot.TodayFirstTokenRequests != 1 || snapshot.TodayFirstTokenMs != 450 || snapshot.TodayImageDurationMs != 2400 {
		t.Fatalf("duration stats = (%d, %d, %d, %d, %d, %d), want (1200, 1, 300, 1, 450, 2400)", snapshot.TodayNonImageDurationMs, snapshot.TodayFirstEventRequests, snapshot.TodayFirstEventMs, snapshot.TodayFirstTokenRequests, snapshot.TodayFirstTokenMs, snapshot.TodayImageDurationMs)
	}
	if snapshot.ActiveUsers != 1 {
		t.Fatalf("ActiveUsers = %d, want 1", snapshot.ActiveUsers)
	}
	if snapshot.AllTimeRequests != 2 || snapshot.RecentRequests1M != 0 || snapshot.RecentRequests10M != 1 {
		t.Fatalf("request totals = (%d, %d, %d), want (2, 0, 1)", snapshot.AllTimeRequests, snapshot.RecentRequests1M, snapshot.RecentRequests10M)
	}
	if snapshot.RecentAccountCost1M != 0 || snapshot.RecentAccountCost10M != 4 {
		t.Fatalf("recent account costs = (%v, %v), want (0, 4)", snapshot.RecentAccountCost1M, snapshot.RecentAccountCost10M)
	}
}

// TestDashboardStoreUsageEstimatesAggregateNonFreeAccounts locks the aggregate
// contract of loadDashboardUsageEstimates: every non-Free OpenAI OAuth account
// is folded into a single non_free estimate with total + 5h windows, while
// per-plan calibration never mixes rates across plan_type values (which is
// observable through the aggregated remaining cost).
func TestDashboardStoreUsageEstimatesAggregateNonFreeAccounts(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	createAccount := func(name, plan string, costPerPercent, growth, last float64) int {
		t.Helper()
		meta := accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{
			GrowthDate:  "2026-08-24",
			DailyGrowth: growth,
			LastPercent: last,
			ObservedAt:  &now,
		}}
		if costPerPercent > 0 {
			meta.SevenDay.CostPerPercent = costPerPercent
			meta.SevenDay.CalibrationWeight = 20
			meta.SevenDay.CalibratedAt = &now
		}
		builder := db.Account.Create().
			SetName(name).
			SetPlatform("openai").
			SetType("oauth").
			SetCredentials(map[string]string{"plan_type": plan}).
			SetUsageEstimateMeta(meta)
		item, err := builder.Save(ctx)
		if err != nil {
			t.Fatalf("create account %s: %v", name, err)
		}
		return item.ID
	}
	loadWindows := func() []appdashboard.UsageEstimateWindow {
		t.Helper()
		estimates, err := NewDashboardStore(db).loadDashboardUsageEstimates(ctx, now, 1)
		if err != nil {
			t.Fatalf("loadDashboardUsageEstimates: %v", err)
		}
		if len(estimates) != 1 || estimates[0].Plan != "non_free" || len(estimates[0].Windows) != 2 ||
			estimates[0].Windows[0].Window != "total" || estimates[0].Windows[1].Window != "5h" {
			t.Fatalf("estimates = %+v, want one non_free estimate with total+5h windows", estimates)
		}
		return estimates[0].Windows
	}
	requireMinutes := func(label string, window appdashboard.UsageEstimateWindow, want float64) {
		t.Helper()
		got := -1.0
		if window.RemainingMinutes != nil {
			got = *window.RemainingMinutes
		}
		if window.RemainingMinutes == nil || got != want {
			t.Fatalf("%s remaining minutes = %v, want %v (%+v)", label, got, want, window)
		}
	}
	plusID := createAccount("plus", "ChatGPT Plus", 0.5, 20, 50) // 7d rate 0.5, +20%, 50% used
	teamID := createAccount("team", "Team", 0.5, 40, 60)         // 7d rate 0.5, +40%, 60% used
	proID := createAccount("pro", "Pro", 1, 30, 70)              // 7d rate 1.0, +30%, 70% used
	disabledID := createAccount("disabled", "Plus", 2, 0, 0)
	if err := db.Account.UpdateOneID(disabledID).SetState(entaccount.StateDisabled).Exec(ctx); err != nil {
		t.Fatalf("disable account: %v", err)
	}

	// Disabled accounts are skipped. plus/team/pro share one 7d pool but keep
	// their own calibrated rates, so the aggregate remaining cost is
	// 0.5*50 + 0.5*40 + 1*30 = $75 (75 min at $1/min) and no 5h sample exists.
	windows := loadWindows()
	if windows[0].Status != "ready" || windows[0].AccountCount != 3 {
		t.Fatalf("total window = %+v, want a ready aggregate of 3 accounts", windows[0])
	}
	requireMinutes("total", windows[0], 75)
	if windows[1].Status != "ready" || windows[1].AccountCount != 0 {
		t.Fatalf("5h window = %+v, want ready/zero because only 7d observations exist", windows[1])
	}

	plusAccount, err := db.Account.Get(ctx, plusID)
	if err != nil {
		t.Fatalf("get plus account: %v", err)
	}
	plusMeta := plusAccount.UsageEstimateMeta
	plusMeta.FiveHour = accountusage.WindowEstimate{
		GrowthDate:        "2026-08-24",
		CostPerPercent:    0.5,
		CalibrationWeight: 20,
		CalibratedAt:      &now,
		ObservedAt:        &now,
	}
	if err := db.Account.UpdateOneID(plusID).SetUsageEstimateMeta(plusMeta).Exec(ctx); err != nil {
		t.Fatalf("set calibrated zero-growth 5h sample: %v", err)
	}
	// A calibrated 5h sample moves plus into the 5h pool; the two pools stay
	// independently calibrated (5h: $50 full / $50 left; 7d: $150 full / $50 left).
	windows = loadWindows()
	if windows[1].Status != "ready" || windows[1].AccountCount != 1 || windows[1].FullCost != 50 || windows[1].DailyGrowthPercent != 0 {
		t.Fatalf("5h window = %+v, want the plus account alone at $50 full cost", windows[1])
	}
	requireMinutes("5h", windows[1], 50)
	if windows[0].AccountCount != 3 {
		t.Fatalf("total window = %+v, want 1 five-hour plus 2 seven-day observations", windows[0])
	}
	requireMinutes("total", windows[0], 100)

	// An uncalibrated Plus account is counted but contributes no rate, so the
	// aggregate only grows by its account count.
	newID := createAccount("new", "Plus", 0, 0, 0)
	windows = loadWindows()
	if windows[0].AccountCount != 4 {
		t.Fatalf("total window = %+v, want the uncalibrated account counted once", windows[0])
	}
	requireMinutes("total", windows[0], 100)

	// Reactivating the calibrated Plus account gives the 7d pool a Plus rate (2.0),
	// and same-plan accounts share that standard: both Plus accounts now contribute
	// 2*100 = $200 each, so the aggregate is 50 (5h) + 200 + 200 + 20 (team) +
	// 30 (pro) = $500.
	if err := db.Account.UpdateOneID(disabledID).SetState(entaccount.StateActive).Exec(ctx); err != nil {
		t.Fatalf("reactivate account: %v", err)
	}
	windows = loadWindows()
	if windows[0].AccountCount != 5 {
		t.Fatalf("total window = %+v, want the reactivated account included", windows[0])
	}
	requireMinutes("total", windows[0], 500)

	// With every calibrated account disabled, only the uncalibrated Plus account
	// remains: the aggregate stays present but insufficient.
	for _, id := range []int{plusID, teamID, proID, disabledID} {
		if err := db.Account.UpdateOneID(id).SetState(entaccount.StateDisabled).Exec(ctx); err != nil {
			t.Fatalf("disable calibrated account %d: %v", id, err)
		}
	}
	windows = loadWindows()
	if windows[0].Status != "insufficient" || windows[0].AccountCount != 1 || windows[0].RemainingCost != nil {
		t.Fatalf("total window = %+v, want insufficient from the uncalibrated account (new_id=%d)", windows[0], newID)
	}
}

func TestDashboardStoreListTrendLogsIncludesSnapshotOnlyRows(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	todayStart := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	endTime := todayStart.Add(24 * time.Hour)

	u, err := db.User.Create().
		SetEmail("trend@example.com").
		SetPasswordHash("secret").
		SetCreatedAt(todayStart.Add(30 * time.Minute)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := db.UsageLog.Create().
		SetBillingEventID("bill_dashboard_relation_trend").
		SetPlatform("openai").
		SetModel("gpt-4.1").
		SetInputTokens(10).
		SetOutputTokens(20).
		SetActualCost(1.5).
		SetTotalCost(2.0).
		SetUser(u).
		SetCreatedAt(todayStart.Add(2 * time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create relation usage log: %v", err)
	}

	if _, err := db.UsageLog.Create().
		SetBillingEventID("bill_dashboard_snapshot_trend").
		SetPlatform("openai").
		SetModel("gpt-image-1").
		SetInputTokens(1).
		SetOutputTokens(2).
		SetActualCost(3.5).
		SetTotalCost(5.0).
		SetUserIDSnapshot(u.ID).
		SetCreatedAt(todayStart.Add(3 * time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create snapshot usage log: %v", err)
	}

	store := NewDashboardStore(db)
	logs, err := store.ListTrendLogs(ctx, todayStart, endTime, u.ID)
	if err != nil {
		t.Fatalf("ListTrendLogs returned error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want 2", len(logs))
	}

	var snapshotOnlyFound bool
	for _, log := range logs {
		if log.Model == "gpt-image-1" {
			snapshotOnlyFound = true
			if log.UserID != u.ID || log.UserEmail != u.Email {
				t.Fatalf("snapshot-only log user fallback = (%d, %q), want (%d, %q)", log.UserID, log.UserEmail, u.ID, u.Email)
			}
		}
	}
	if !snapshotOnlyFound {
		t.Fatal("snapshot-only usage log was not returned")
	}
}
