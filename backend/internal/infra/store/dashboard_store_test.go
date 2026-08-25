package store

import (
	"context"
	"testing"
	"time"

	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/accountusage"
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

func TestDashboardStoreUsageEstimatesAggregatePlusTeamAndPro(t *testing.T) {
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
	plusID := createAccount("plus", "ChatGPT Plus", 0.5, 20, 50) // full $50, remaining $25
	teamID := createAccount("team", "Team", 0.5, 40, 60)         // full $50, remaining $20
	proID := createAccount("pro", "Pro", 1, 30, 70)              // full $100, remaining $30
	disabledID := createAccount("disabled", "Plus", 2, 0, 0)
	if err := db.Account.UpdateOneID(disabledID).SetState(entaccount.StateDisabled).Exec(ctx); err != nil {
		t.Fatalf("disable account: %v", err)
	}

	estimates, err := NewDashboardStore(db).loadDashboardUsageEstimates(ctx, now, 1)
	if err != nil {
		t.Fatalf("loadDashboardUsageEstimates: %v", err)
	}
	if len(estimates) != 2 {
		t.Fatalf("estimates = %+v, want Plus and Pro", estimates)
	}
	if estimates[0].Plan != "plus" || len(estimates[0].Windows) != 1 {
		t.Fatalf("plus estimate = %+v, want only 7d", estimates[0])
	}
	plusWindow := estimates[0].Windows[0]
	if plusWindow.Window != "7d" || plusWindow.Status != "ready" || plusWindow.DailyGrowthPercent != 30 || plusWindow.FullCost != 100 || plusWindow.RemainingMinutes == nil || *plusWindow.RemainingMinutes != 45 {
		t.Fatalf("plus 7d estimate = %+v, want +30%% $100 45min", plusWindow)
	}
	if estimates[1].Plan != "pro" || len(estimates[1].Windows) != 1 {
		t.Fatalf("pro estimate = %+v, want only 7d", estimates[1])
	}
	proWindow := estimates[1].Windows[0]
	if proWindow.Window != "7d" || proWindow.Status != "ready" || proWindow.DailyGrowthPercent != 30 || proWindow.FullCost != 200 || proWindow.RemainingMinutes == nil || *proWindow.RemainingMinutes != 75 {
		t.Fatalf("pro 7d estimate = %+v, want +30%% $200 75min", proWindow)
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
	estimates, err = NewDashboardStore(db).loadDashboardUsageEstimates(ctx, now, 1)
	if err != nil {
		t.Fatalf("load estimates with calibrated zero-growth 5h: %v", err)
	}
	if len(estimates[0].Windows) != 2 || estimates[0].Windows[0].Window != "5h" || estimates[0].Windows[0].Status != "ready" || estimates[0].Windows[0].DailyGrowthPercent != 0 || estimates[0].Windows[0].FullCost != 50 ||
		len(estimates[1].Windows) != 2 || estimates[1].Windows[0].Window != "5h" || estimates[1].Windows[0].Status != "ready" {
		t.Fatalf("calibrated zero-growth 5h should remain usable: %+v", estimates)
	}

	newID := createAccount("new", "Plus", 0, 0, 0)
	estimates, err = NewDashboardStore(db).loadDashboardUsageEstimates(ctx, now, 1)
	if err != nil {
		t.Fatalf("load estimates with partial new account: %v", err)
	}
	plusWindow = estimates[0].Windows[1]
	if plusWindow.Status != "ready" || plusWindow.FullCost != 150 || plusWindow.DailyGrowthPercent != 20 || plusWindow.RemainingMinutes == nil || *plusWindow.RemainingMinutes != 95 {
		t.Fatalf("new account should use the shared Plus standard: %+v", plusWindow)
	}

	if err := db.Account.UpdateOneID(disabledID).SetState(entaccount.StateActive).Exec(ctx); err != nil {
		t.Fatalf("reactivate account: %v", err)
	}
	estimates, err = NewDashboardStore(db).loadDashboardUsageEstimates(ctx, now, 1)
	if err != nil {
		t.Fatalf("load estimates after reactivation: %v", err)
	}
	if estimates[0].Windows[1].Status != "ready" || estimates[0].Windows[1].FullCost != 425 ||
		estimates[1].Windows[1].Status != "ready" || estimates[1].Windows[1].FullCost != 525 {
		t.Fatalf("reactivated calibrated account should rejoin estimates: %+v", estimates)
	}

	for _, id := range []int{plusID, teamID, proID, disabledID} {
		if err := db.Account.UpdateOneID(id).SetState(entaccount.StateDisabled).Exec(ctx); err != nil {
			t.Fatalf("disable calibrated account %d: %v", id, err)
		}
	}
	estimates, err = NewDashboardStore(db).loadDashboardUsageEstimates(ctx, now, 1)
	if err != nil {
		t.Fatalf("load all-new estimates: %v", err)
	}
	if len(estimates) != 1 || estimates[0].Plan != "plus" || len(estimates[0].Windows) != 1 || estimates[0].Windows[0].Status != "insufficient" {
		t.Fatalf("all-new active pool should be insufficient: %+v (new_id=%d)", estimates, newID)
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
