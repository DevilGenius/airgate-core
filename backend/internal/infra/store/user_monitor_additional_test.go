package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	entmonitorevent "github.com/DevilGenius/airgate-core/ent/monitorevent"
	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	appuser "github.com/DevilGenius/airgate-core/internal/app/user"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

func TestUserStoreCRUDKeysAndBalance(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	group, err := db.Group.Create().
		SetName("User Group").
		SetPlatform("openai").
		SetRateMultiplier(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	allowedGroup, err := db.Group.Create().
		SetName("Allowed User Group").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create allowed group: %v", err)
	}

	store := NewUserStore(db)
	created, err := store.Create(ctx, appuser.Mutation{
		Email:          storePtr("user-store@example.com"),
		Username:       storePtr("User Store"),
		PasswordHash:   storePtr("hash"),
		Role:           storePtr("user"),
		MaxConcurrency: storePtr(4),
		GroupRates:     map[int64]float64{int64(group.ID): 3.25},
		HasGroupRates:  true,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.Email != "user-store@example.com" || created.Username != "User Store" ||
		created.Role != "user" || created.MaxConcurrency != 4 || created.GroupRates[int64(group.ID)] != 3.25 {
		t.Fatalf("created user = %+v", created)
	}
	created.GroupRates[int64(group.ID)] = 99
	found, err := store.FindByID(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if found.GroupRates[int64(group.ID)] != 3.25 {
		t.Fatalf("group rates clone leaked mutation: %+v", found.GroupRates)
	}
	if _, err := store.FindByID(ctx, 999999, false); !errors.Is(err, appuser.ErrUserNotFound) {
		t.Fatalf("FindByID missing error = %v, want ErrUserNotFound", err)
	}

	exists, err := store.EmailExists(ctx, created.Email)
	if err != nil || !exists {
		t.Fatalf("EmailExists existing = %v err %v, want true nil", exists, err)
	}
	exists, err = store.EmailExists(ctx, "missing-user-store@example.com")
	if err != nil || exists {
		t.Fatalf("EmailExists missing = %v err %v, want false nil", exists, err)
	}

	list, total, err := store.List(ctx, appuser.ListFilter{
		Page: 1, PageSize: 20, Keyword: "user-store", Status: "active", Role: "user",
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("List total %d list %+v, want created user", total, list)
	}
	overrides, err := store.ListWithGroupRateOverride(ctx, int64(group.ID))
	if err != nil {
		t.Fatalf("ListWithGroupRateOverride returned error: %v", err)
	}
	if len(overrides) != 1 || overrides[0].UserID != created.ID || overrides[0].Rate != 3.25 {
		t.Fatalf("rate overrides = %+v, want created user", overrides)
	}
	if _, err := db.User.Create().
		SetEmail("invalid-rate@example.com").
		SetPasswordHash("hash").
		SetGroupRates(map[int64]float64{int64(group.ID): 0}).
		Save(ctx); err != nil {
		t.Fatalf("create invalid rate user: %v", err)
	}
	overrides, err = store.ListWithGroupRateOverride(ctx, int64(group.ID))
	if err != nil {
		t.Fatalf("ListWithGroupRateOverride after invalid returned error: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("rate overrides after invalid = %+v, want invalid multiplier ignored", overrides)
	}

	updated, err := store.Update(ctx, created.ID, appuser.Mutation{
		Username:           storePtr("Updated User"),
		PasswordHash:       storePtr("new-hash"),
		Role:               storePtr("admin"),
		MaxConcurrency:     storePtr(9),
		GroupRates:         map[int64]float64{int64(group.ID): 4.5},
		HasGroupRates:      true,
		AllowedGroupIDs:    []int64{int64(allowedGroup.ID)},
		HasAllowedGroupIDs: true,
		Status:             storePtr("disabled"),
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Username != "Updated User" || updated.PasswordHash != "new-hash" || updated.Role != "admin" ||
		updated.MaxConcurrency != 9 || updated.GroupRates[int64(group.ID)] != 4.5 ||
		len(updated.AllowedGroupIDs) != 1 || updated.AllowedGroupIDs[0] != int64(allowedGroup.ID) ||
		updated.Status != "disabled" {
		t.Fatalf("updated user = %+v", updated)
	}
	if _, err := store.Update(ctx, 999999, appuser.Mutation{Username: storePtr("missing")}); !errors.Is(err, appuser.ErrUserNotFound) {
		t.Fatalf("Update missing error = %v, want ErrUserNotFound", err)
	}

	balanced, err := store.UpdateBalance(ctx, created.ID, appuser.BalanceUpdate{
		Action: "add", Amount: 25, BeforeBalance: 0, AfterBalance: 25, Remark: "top up",
	})
	if err != nil {
		t.Fatalf("UpdateBalance returned error: %v", err)
	}
	if balanced.Balance != 25 {
		t.Fatalf("balanced user = %+v", balanced)
	}
	if _, err := store.UpdateBalance(ctx, 999999, appuser.BalanceUpdate{Action: "add"}); !errors.Is(err, appuser.ErrUserNotFound) {
		t.Fatalf("UpdateBalance missing error = %v, want ErrUserNotFound", err)
	}
	balanceLogs, balanceTotal, err := store.ListBalanceLogs(ctx, created.ID, 1, 20)
	if err != nil {
		t.Fatalf("ListBalanceLogs returned error: %v", err)
	}
	if balanceTotal != 1 || len(balanceLogs) != 1 || balanceLogs[0].Amount != 25 || balanceLogs[0].Action != "add" {
		t.Fatalf("balance logs = total %d list %+v", balanceTotal, balanceLogs)
	}

	expiresAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	key, err := db.APIKey.Create().
		SetName("user-store-key").
		SetKeyHint("sk-user").
		SetKeyHash("hash-user-store-key").
		SetUserID(created.ID).
		SetGroupID(group.ID).
		SetIPWhitelist([]string{"127.0.0.1"}).
		SetIPBlacklist([]string{"10.0.0.1"}).
		SetQuotaUsd(100).
		SetUsedQuota(5).
		SetUsedQuotaActual(2.5).
		SetSellRate(1.25).
		SetBalanceAlertEnabled(true).
		SetBalanceAlertEmail("user-alert@example.com").
		SetBalanceAlertThreshold(10).
		SetBalanceAlertNotified(true).
		SetExpiresAt(expiresAt).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user key: %v", err)
	}
	todayStart := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	createAccountUsageLog(t, db, "user_store_key_today", created.ID, key.ID, 0, group.ID, "gpt-5", todayStart.Add(time.Hour), 1, 2, 0, 0, 1, 1.5, 2)
	createAccountUsageLog(t, db, "user_store_key_old", created.ID, key.ID, 0, group.ID, "gpt-5", todayStart.AddDate(0, 0, -5), 1, 2, 0, 0, 3, 4.5, 6)

	name, err := store.GetAPIKeyName(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyName returned error: %v", err)
	}
	if name != "user-store-key" {
		t.Fatalf("key name = %q, want user-store-key", name)
	}
	info, err := store.GetAPIKeyInfo(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyInfo returned error: %v", err)
	}
	if info.Name != "user-store-key" || info.QuotaUSD != 100 || info.UsedQuota != 5 ||
		info.SellRate != 1.25 || info.GroupRate != 4.5 || info.Platform != "openai" ||
		info.ExpiresAt == nil || !info.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("key info = %+v", info)
	}
	keys, keyTotal, err := store.ListAPIKeys(ctx, created.ID, 1, 20, todayStart)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if keyTotal != 1 || len(keys) != 1 || keys[0].GroupID == nil || *keys[0].GroupID != group.ID ||
		keys[0].TodayCost != 2 || keys[0].TodayActualCost != 1.5 ||
		keys[0].ThirtyDayCost != 8 || keys[0].ThirtyDayActualCost != 6 {
		t.Fatalf("ListAPIKeys total %d list %+v", keyTotal, keys)
	}
	keys[0].IPWhitelist[0] = "mutated"
	keysAgain, _, err := store.ListAPIKeys(ctx, created.ID, 1, 20, todayStart)
	if err != nil {
		t.Fatalf("ListAPIKeys after clone mutation returned error: %v", err)
	}
	if keysAgain[0].IPWhitelist[0] != "127.0.0.1" {
		t.Fatalf("API key whitelist clone leaked mutation: %+v", keysAgain[0].IPWhitelist)
	}

	if err := store.UpdateBalanceAlert(ctx, created.ID, 12.5); err != nil {
		t.Fatalf("UpdateBalanceAlert returned error: %v", err)
	}
	if err := store.SetBalanceAlertNotified(ctx, created.ID, true); err != nil {
		t.Fatalf("SetBalanceAlertNotified returned error: %v", err)
	}
	alerted, err := store.FindByID(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("FindByID after alert updates returned error: %v", err)
	}
	if alerted.BalanceAlertThreshold != 12.5 || !alerted.BalanceAlertNotified {
		t.Fatalf("alert user = %+v", alerted)
	}
}

func TestMonitorStorePureHelperBranches(t *testing.T) {
	if got := defaultMonitorRecoveryMode(""); got != monitoring.RecoveryModeNone {
		t.Fatalf("default recovery mode = %q", got)
	}
	if got := defaultMonitorRecoveryMode(monitoring.RecoveryModeManual); got != monitoring.RecoveryModeManual {
		t.Fatalf("explicit recovery mode = %q", got)
	}

	counts := []appmonitor.SubjectCount{
		{ID: 2, Name: "two", Count: 3},
		{ID: 1, Name: "one", Count: 3},
		{ID: 3, Name: "three", Count: 4},
	}
	sortSubjectCounts(counts)
	if counts[0].ID != 3 || counts[1].ID != 1 || counts[2].ID != 2 {
		t.Fatalf("sorted subject counts = %+v", counts)
	}
	limited := limitSubjectCounts(counts, 2)
	if len(limited) != 2 || limited[0].ID != 3 || len(limitSubjectCounts(counts, 0)) != 3 {
		t.Fatalf("limited subject counts = %+v", limited)
	}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	accountID := 42
	row := &ent.MonitorEvent{
		ID:                  7,
		Type:                entmonitorevent.TypeSystemError,
		Severity:            entmonitorevent.SeverityCritical,
		Status:              entmonitorevent.StatusActive,
		RecoveryMode:        entmonitorevent.RecoveryModeManual,
		Source:              monitoring.SourceMonitorWorker,
		SubjectType:         monitoring.SubjectAccount,
		SubjectID:           "42",
		Hash:                "hash",
		Title:               "title",
		Message:             "message",
		AccountID:           &accountID,
		AccountNameSnapshot: "account",
		Platform:            "openai",
		PluginID:            "openai",
		TaskType:            "health",
		ErrorCode:           "failed",
		CreatedAt:           now,
		UpdatedAt:           now,
		ExpiresAt:           now.Add(time.Hour),
	}
	mapped := mapMonitorEvent(row)
	if mapped.ID != 7 || mapped.AccountID == nil || *mapped.AccountID != 42 || mapped.Detail == nil || len(mapped.Detail) != 0 {
		t.Fatalf("mapped monitor event = %+v", mapped)
	}
}

func TestMonitorStoreEventsLifecycleAndRequests(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	store := NewMonitorStore(db)
	var nilStore *MonitorStore
	if err := nilStore.InsertBatch(ctx, []appmonitor.QueuedEvent{{}}); err != nil {
		t.Fatalf("nil InsertBatch returned error: %v", err)
	}
	if summary, err := nilStore.Summary(ctx); err != nil || summary.ActiveTotal != 0 {
		t.Fatalf("nil Summary = %+v err %v", summary, err)
	}
	if got := mapMonitorEvent(nil); got.ID != 0 || got.Detail != nil {
		t.Fatalf("mapMonitorEvent(nil) = %+v", got)
	}
	if truncateStoreString("abcdef", 3) != "abc" || truncateStoreString("abcdef", 0) != "abcdef" {
		t.Fatal("truncateStoreString returned unexpected values")
	}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	accountID := 101
	event := appmonitor.QueuedEvent{Event: appmonitor.Event{
		Type:                monitoring.TypeUpstreamAccountError,
		Severity:            monitoring.SeverityError,
		RecoveryMode:        monitoring.RecoveryModeManual,
		Source:              monitoring.SourceScheduler,
		SubjectType:         monitoring.SubjectAccount,
		SubjectID:           "101",
		Hash:                "monitor-hash",
		Title:               "Account failed",
		Message:             "upstream failure",
		AccountID:           &accountID,
		AccountNameSnapshot: "Account 101",
		Platform:            "openai",
		PluginID:            "openai",
		TaskType:            "health_check",
		ErrorCode:           "upstream_500",
		CreatedAt:           now,
		UpdatedAt:           now,
		AutoResolveAt:       storePtr(now.Add(time.Hour)),
		ExpiresAt:           now.Add(24 * time.Hour),
		Detail:              map[string]interface{}{"attempt": float64(1)},
	}}
	if err := store.InsertBatch(ctx, []appmonitor.QueuedEvent{{Event: appmonitor.Event{Hash: ""}}, event}); err != nil {
		t.Fatalf("InsertBatch create returned error: %v", err)
	}
	active, err := db.MonitorEvent.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query active monitor event: %v", err)
	}
	if active.NextNotifyAt == nil || !active.NextNotifyAt.Equal(now) {
		t.Fatalf("created NextNotifyAt = %v, want %v", active.NextNotifyAt, now)
	}
	if _, err := db.MonitorEvent.Create().
		SetType(entmonitorevent.TypeUpstreamAccountError).
		SetSeverity(entmonitorevent.SeverityError).
		SetStatus(entmonitorevent.StatusActive).
		SetRecoveryMode(entmonitorevent.RecoveryModeManual).
		SetSource(monitoring.SourceScheduler).
		SetSubjectType(monitoring.SubjectAccount).
		SetSubjectID("101").
		SetHash("monitor-hash").
		SetTitle("Duplicate").
		SetMessage("duplicate").
		SetUpdatedAt(now.Add(-time.Minute)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create duplicate monitor event: %v", err)
	}
	updatedEvent := event
	updatedEvent.Message = "updated message"
	updatedEvent.UpdatedAt = now.Add(time.Minute)
	updatedEvent.AutoResolveAt = nil
	if err := store.InsertBatch(ctx, []appmonitor.QueuedEvent{updatedEvent}); err != nil {
		t.Fatalf("InsertBatch update returned error: %v", err)
	}
	activeCount, err := db.MonitorEvent.Query().
		Where(entmonitorevent.HashEQ("monitor-hash"), entmonitorevent.StatusEQ(entmonitorevent.StatusActive)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count active monitor events: %v", err)
	}
	resolvedCount, err := db.MonitorEvent.Query().
		Where(entmonitorevent.HashEQ("monitor-hash"), entmonitorevent.StatusEQ(entmonitorevent.StatusResolved)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count resolved monitor events: %v", err)
	}
	if activeCount != 1 || resolvedCount != 1 {
		t.Fatalf("duplicate resolution active=%d resolved=%d, want 1/1", activeCount, resolvedCount)
	}

	got, err := store.Get(ctx, active.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Hash != "monitor-hash" || got.Detail["attempt"] != float64(1) {
		t.Fatalf("Get = %+v", got)
	}
	if _, err := store.Get(ctx, 0); !errors.Is(err, appmonitor.ErrEventNotFound) {
		t.Fatalf("Get invalid error = %v, want ErrEventNotFound", err)
	}
	from := now.Add(-time.Hour)
	to := now.Add(2 * time.Hour)
	list, err := store.List(ctx, appmonitor.ListFilter{
		Status:   monitoring.StatusActive,
		Severity: monitoring.SeverityError + " " + monitoring.SeverityWarning,
		Type:     monitoring.TypeUpstreamAccountError + " " + monitoring.TypeSystemError,
		Source:   monitoring.SourceScheduler, SubjectType: monitoring.SubjectAccount, AccountID: &accountID,
		Platform: "openai", PluginID: "openai", TaskType: "health_check", ErrorCode: "upstream_500",
		From: &from, To: &to, Limit: 1,
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(list.List) != 1 || list.List[0].Hash != "monitor-hash" {
		t.Fatalf("List = %+v", list)
	}
	summary, err := store.Summary(ctx)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.ActiveTotal != 1 || summary.ErrorActiveTotal != 1 || len(summary.ByType) == 0 ||
		len(summary.TopAccounts) == 0 || len(summary.Recent) == 0 {
		t.Fatalf("Summary = %+v", summary)
	}
	due, err := store.ListNotifyDue(ctx, now.Add(2*time.Minute), 0)
	if err != nil {
		t.Fatalf("ListNotifyDue returned error: %v", err)
	}
	if len(due) != 1 || due[0].Hash != "monitor-hash" {
		t.Fatalf("ListNotifyDue = %+v", due)
	}
	if err := store.MarkNotified(ctx, due[0].ID, now.Add(2*time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("MarkNotified returned error: %v", err)
	}
	if err := store.MarkNotifyFailed(ctx, due[0].ID, now.Add(30*time.Minute), strings.Repeat("x", 600)); err != nil {
		t.Fatalf("MarkNotifyFailed returned error: %v", err)
	}
	failed, err := db.MonitorEvent.Get(ctx, due[0].ID)
	if err != nil {
		t.Fatalf("get failed notification event: %v", err)
	}
	if len([]rune(failed.NotifyError)) != 500 {
		t.Fatalf("NotifyError len = %d, want 500", len([]rune(failed.NotifyError)))
	}
	if err := store.MarkNotified(ctx, 0, now, now); err != nil {
		t.Fatalf("MarkNotified invalid returned error: %v", err)
	}
	if err := store.MarkNotifyFailed(ctx, 0, now, "ignored"); err != nil {
		t.Fatalf("MarkNotifyFailed invalid returned error: %v", err)
	}

	if err := store.Resolve(ctx, due[0].ID); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if err := store.Resolve(ctx, 999999); !errors.Is(err, appmonitor.ErrEventNotFound) {
		t.Fatalf("Resolve missing error = %v, want ErrEventNotFound", err)
	}
	auto, err := db.MonitorEvent.Create().
		SetType(entmonitorevent.TypeSystemError).
		SetSeverity(entmonitorevent.SeverityCritical).
		SetStatus(entmonitorevent.StatusActive).
		SetRecoveryMode(entmonitorevent.RecoveryModeSuccess).
		SetSource(monitoring.SourceMonitorWorker).
		SetSubjectType(monitoring.SubjectSystem).
		SetSubjectID("system").
		SetHash("auto-hash").
		SetTitle("Auto").
		SetMessage("auto").
		SetAutoResolveAt(now.Add(-time.Minute)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create auto event: %v", err)
	}
	resolved, err := store.AutoResolveDue(ctx, now, 0)
	if err != nil {
		t.Fatalf("AutoResolveDue returned error: %v", err)
	}
	if resolved != 1 {
		t.Fatalf("AutoResolveDue count = %d, want 1", resolved)
	}
	autoRow, err := db.MonitorEvent.Get(ctx, auto.ID)
	if err != nil {
		t.Fatalf("get auto event: %v", err)
	}
	if autoRow.Status != entmonitorevent.StatusResolved {
		t.Fatalf("auto event status = %s, want resolved", autoRow.Status)
	}

	subject, err := db.MonitorEvent.Create().
		SetType(entmonitorevent.TypePluginError).
		SetSeverity(entmonitorevent.SeverityError).
		SetStatus(entmonitorevent.StatusActive).
		SetRecoveryMode(entmonitorevent.RecoveryModeManual).
		SetSource(monitoring.SourcePluginManager).
		SetSubjectType(monitoring.SubjectPlugin).
		SetSubjectID("plugin-1").
		SetHash("subject-hash").
		SetTitle("Subject").
		SetMessage("subject").
		SetPluginID("plugin-1").
		SetTaskType("install").
		SetErrorCode("plugin_failed").
		SetExpiresAt(now.Add(24 * time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create subject event: %v", err)
	}
	if err := store.ResolveBySubject(ctx, monitoring.ResolveQuery{
		Hash: "subject-hash", Type: monitoring.TypePluginError, SubjectType: monitoring.SubjectPlugin,
		SubjectID: "plugin-1", PluginID: "plugin-1", TaskType: "install", ErrorCode: "plugin_failed",
	}); err != nil {
		t.Fatalf("ResolveBySubject returned error: %v", err)
	}
	subjectRow, err := db.MonitorEvent.Get(ctx, subject.ID)
	if err != nil {
		t.Fatalf("get subject event: %v", err)
	}
	if subjectRow.Status != entmonitorevent.StatusResolved {
		t.Fatalf("subject event status = %s, want resolved", subjectRow.Status)
	}
	if err := store.ResolveBySubject(ctx, monitoring.ResolveQuery{}); err != nil {
		t.Fatalf("ResolveBySubject empty returned error: %v", err)
	}

	if _, err := db.MonitorEvent.Create().
		SetType(entmonitorevent.TypeTaskError).
		SetSeverity(entmonitorevent.SeverityWarning).
		SetStatus(entmonitorevent.StatusActive).
		SetRecoveryMode(entmonitorevent.RecoveryModeNone).
		SetSource(monitoring.SourceTaskRunner).
		SetHash("expired-hash").
		SetTitle("Expired").
		SetMessage("expired").
		SetExpiresAt(now.Add(-time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create expired monitor event: %v", err)
	}
	deleted, err := store.CleanupExpired(ctx, now, 0)
	if err != nil {
		t.Fatalf("CleanupExpired returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("CleanupExpired deleted = %d, want 1", deleted)
	}

	testMonitorRequestStore(t, store, now)
}

func testMonitorRequestStore(t *testing.T, store *MonitorStore, now time.Time) {
	t.Helper()
	ctx := context.Background()
	var nilStore *MonitorStore
	if err := nilStore.InsertRequestBatch(ctx, []appmonitor.QueuedRequestEvent{{}}); err != nil {
		t.Fatalf("nil InsertRequestBatch returned error: %v", err)
	}
	if got := mapMonitorRequestEvent(nil); got.ID != 0 || got.Detail != nil {
		t.Fatalf("mapMonitorRequestEvent(nil) = %+v", got)
	}

	apiKeyID := 201
	userID := 301
	groupID := 401
	accountID := 501
	httpStatus := 429
	upstreamStatus := 503
	requests := []appmonitor.QueuedRequestEvent{
		{},
		{RequestEvent: appmonitor.RequestEvent{
			Type: "api_request_error", Severity: "warning", Source: "forwarder", Hash: "req-hash-1",
			Fingerprint: "fingerprint-1", Title: "Rate limited", Message: "rate limited", RequestID: "req-1",
			APIKeyID: &apiKeyID, APIKeyNameSnapshot: "request-key", UserID: &userID, UserEmailSnapshot: "request-user@example.com",
			GroupID: &groupID, AccountID: &accountID, AccountNameSnapshot: "request-account", Platform: "openai",
			PluginID: "openai", Method: "POST", Endpoint: "/v1/chat/completions", Model: "gpt-5",
			HTTPStatus: &httpStatus, UpstreamStatus: &upstreamStatus, ErrorCode: "rate_limit",
			DurationMS: 1234, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Minute),
			Detail: map[string]interface{}{"retry": float64(1)},
		}},
		{RequestEvent: appmonitor.RequestEvent{
			Type: "client_closed_request", Severity: "info", Source: "forwarder", Hash: "req-hash-2",
			Title: "Client closed", Message: "closed", RequestID: "req-2", Platform: "openai",
			Method: "GET", Endpoint: "/v1/models", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
		}},
	}
	if err := store.InsertRequestBatch(ctx, requests); err != nil {
		t.Fatalf("InsertRequestBatch returned error: %v", err)
	}
	filtered, err := store.ListRequests(ctx, appmonitor.RequestListFilter{
		Severity: "warning info", Type: "api_request_error client_closed_request", Source: "forwarder", APIKeyID: &apiKeyID,
		GroupID: &groupID, AccountID: &accountID, Platform: "openai", PluginID: "openai",
		Method: "POST", Endpoint: "/v1/chat/completions", Model: "gpt-5",
		HTTPStatus: "4xx !404", UpstreamStatus: &upstreamStatus, ErrorCode: "rate_limit",
		From: storePtr(now.Add(-3 * time.Hour)), To: storePtr(now), Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRequests filtered returned error: %v", err)
	}
	if len(filtered.List) != 1 || filtered.List[0].Hash != "req-hash-1" ||
		filtered.List[0].Detail["retry"] != float64(1) {
		t.Fatalf("filtered request list = %+v", filtered)
	}
	excluded, err := store.ListRequests(ctx, appmonitor.RequestListFilter{HTTPStatus: "!4xx", Limit: 10})
	if err != nil {
		t.Fatalf("ListRequests excluded status returned error: %v", err)
	}
	if len(excluded.List) != 1 || excluded.List[0].Hash != "req-hash-2" {
		t.Fatalf("excluded request list = %+v", excluded)
	}
	firstPage, err := store.ListRequests(ctx, appmonitor.RequestListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListRequests first page returned error: %v", err)
	}
	if len(firstPage.List) != 1 || !firstPage.HasMore || firstPage.NextCursor == nil {
		t.Fatalf("first request page = %+v", firstPage)
	}
	secondPage, err := store.ListRequests(ctx, appmonitor.RequestListFilter{Limit: 1, Cursor: firstPage.NextCursor})
	if err != nil {
		t.Fatalf("ListRequests second page returned error: %v", err)
	}
	if len(secondPage.List) != 1 || secondPage.HasMore {
		t.Fatalf("second request page = %+v", secondPage)
	}
	requestSummary, err := store.RequestSummary(ctx)
	if err != nil {
		t.Fatalf("RequestSummary returned error: %v", err)
	}
	if requestSummary.WarningTotal != 1 || requestSummary.InfoTotal != 1 {
		t.Fatalf("RequestSummary = %+v", requestSummary)
	}
	deletedBefore, err := store.ClearRequestEvents(ctx, storePtr(now.Add(-90*time.Minute)))
	if err != nil {
		t.Fatalf("ClearRequestEvents before returned error: %v", err)
	}
	if deletedBefore != 1 {
		t.Fatalf("ClearRequestEvents before deleted = %d, want 1", deletedBefore)
	}
	cleaned, err := store.CleanupExpiredRequests(ctx, now, 0)
	if err != nil {
		t.Fatalf("CleanupExpiredRequests returned error: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("CleanupExpiredRequests cleaned = %d, want 0 after old expired row was cleared", cleaned)
	}
	if err := store.InsertRequestBatch(ctx, []appmonitor.QueuedRequestEvent{{
		RequestEvent: appmonitor.RequestEvent{
			Type: "api_request_error", Severity: "warning", Source: "forwarder", Hash: "req-hash-3",
			Title: "Expired request", Message: "expired request", CreatedAt: now, ExpiresAt: now.Add(-time.Minute),
		},
	}}); err != nil {
		t.Fatalf("InsertRequestBatch expired request returned error: %v", err)
	}
	cleaned, err = store.CleanupExpiredRequests(ctx, now, 0)
	if err != nil {
		t.Fatalf("CleanupExpiredRequests delete branch returned error: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("CleanupExpiredRequests cleaned = %d, want 1", cleaned)
	}
	deletedAll, err := store.ClearRequestEvents(ctx, nil)
	if err != nil {
		t.Fatalf("ClearRequestEvents all returned error: %v", err)
	}
	if deletedAll != 1 {
		t.Fatalf("ClearRequestEvents all deleted = %d, want 1", deletedAll)
	}
}
