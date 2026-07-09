package store

import (
	"context"
	"errors"
	"testing"
	"time"

	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	appgroup "github.com/DevilGenius/airgate-core/internal/app/group"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestGroupStoreListFindAndListAvailable(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "group-available@example.com")
	alpha, err := db.Group.Create().
		SetName("Alpha Public").
		SetPlatform("openai").
		SetServiceTier("premium").
		SetSortWeight(10).
		Save(ctx)
	if err != nil {
		t.Fatalf("create alpha group: %v", err)
	}
	beta, err := db.Group.Create().
		SetName("Beta Exclusive").
		SetPlatform("openai").
		SetServiceTier("premium").
		SetIsExclusive(true).
		AddAllowedUserIDs(user.ID).
		SetSortWeight(30).
		Save(ctx)
	if err != nil {
		t.Fatalf("create beta group: %v", err)
	}
	if _, err := db.Group.Create().
		SetName("Gamma Exclusive Hidden").
		SetPlatform("openai").
		SetIsExclusive(true).
		SetSortWeight(20).
		Save(ctx); err != nil {
		t.Fatalf("create gamma group: %v", err)
	}
	if _, err := db.Group.Create().
		SetName("Claude Public").
		SetPlatform("claude").
		SetServiceTier("standard").
		Save(ctx); err != nil {
		t.Fatalf("create claude group: %v", err)
	}

	store := NewGroupStore(db)
	list, total, err := store.List(ctx, appgroup.ListFilter{
		Page: 1, PageSize: 20, Keyword: "a", Platform: "openai", ServiceTier: "premium",
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("List total=%d len=%d items=%+v, want two openai premium groups", total, len(list), list)
	}
	if list[0].ID != beta.ID || list[1].ID != alpha.ID {
		t.Fatalf("List order IDs = %d,%d want beta,alpha", list[0].ID, list[1].ID)
	}

	available, total, err := store.ListAvailable(ctx, appgroup.AvailableFilter{
		UserID: user.ID, Page: 1, PageSize: 20, Keyword: "a", Platform: "openai",
	})
	if err != nil {
		t.Fatalf("ListAvailable returned error: %v", err)
	}
	if total != 2 || len(available) != 2 || available[0].ID != beta.ID || available[1].ID != alpha.ID {
		t.Fatalf("available groups = total %d list %+v, want allowed exclusive plus public", total, available)
	}

	found, err := store.FindByID(ctx, alpha.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if found.Name != "Alpha Public" || found.Platform != "openai" {
		t.Fatalf("FindByID = %+v", found)
	}
	if _, err := store.FindByID(ctx, 999999); !errors.Is(err, appgroup.ErrGroupNotFound) {
		t.Fatalf("FindByID missing error = %v, want ErrGroupNotFound", err)
	}
}

func TestGroupStoreCreateUpdateCopiesAndClonesConfig(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	sourceGroup, err := db.Group.Create().
		SetName("Source").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create source group: %v", err)
	}
	sourceAccount, err := db.Account.Create().
		SetName("Source Account").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-source"}).
		AddGroupIDs(sourceGroup.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create source account: %v", err)
	}
	otherPlatformGroup, err := db.Group.Create().
		SetName("Other Platform").
		SetPlatform("claude").
		Save(ctx)
	if err != nil {
		t.Fatalf("create other platform group: %v", err)
	}

	rate := 2.5
	createDSL := sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		ID:             "rule-1",
		Operation:      "chat",
		Model:          sdk.DispatchModel{StripSuffix: "-mini"},
		TimeoutProfile: "long",
		Gate:           sdk.DispatchGate{RequiredOperation: "paid"},
		Candidates:     []sdk.DispatchCandidate{{Scheduling: "gpt-5-mini", Wire: "gpt-5"}},
		When: sdk.DispatchWhen{
			Methods:       []string{"POST"},
			Paths:         []string{"/v1/chat/completions"},
			PathPrefixes:  []string{"/v1/"},
			Models:        []string{"gpt-5"},
			ModelPrefixes: []string{"gpt-"},
			ModelSuffixes: []string{"-mini"},
		},
	}}}
	created, err := NewGroupStore(db).Create(ctx, appgroup.CreateInput{
		Name:                     "Created",
		Platform:                 "openai",
		RateMultiplier:           &rate,
		IsExclusive:              true,
		StatusVisible:            true,
		SubscriptionType:         "subscription",
		Quotas:                   map[string]any{"daily": float64(100)},
		ModelRouting:             map[string][]int64{"gpt-5": {int64(sourceAccount.ID)}},
		ModelPolicy:              modelpolicy.Policy{Allow: []string{"gpt-5*"}},
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{"oauth": {Deny: []string{"gpt-4*"}}},
		DispatchDSL:              createDSL,
		OperationPolicies:        map[string]bool{"images.generate": true},
		PluginSettings:           map[string]map[string]string{"openai": {"images": "true"}},
		ServiceTier:              "premium",
		ForceInstructions:        "use paid tier",
		Note:                     "created note",
		SortWeight:               88,
		CopyAccountsFromGroupIDs: []int{sourceGroup.ID, sourceGroup.ID},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.RateMultiplier != rate || !created.IsExclusive || created.SubscriptionType != "subscription" {
		t.Fatalf("created group missing scalar fields: %+v", created)
	}
	if created.Quotas["daily"] != float64(100) || created.ModelRouting["gpt-5"][0] != int64(sourceAccount.ID) {
		t.Fatalf("created group missing config fields: %+v", created)
	}
	if related, err := sourceAccount.QueryGroups().IDs(ctx); err != nil || len(related) != 2 {
		t.Fatalf("source account groups = %v, err=%v; want source plus copied target", related, err)
	}

	if _, err := NewGroupStore(db).Create(ctx, appgroup.CreateInput{
		Name: "Missing Source", Platform: "openai", CopyAccountsFromGroupIDs: []int{999999},
	}); !errors.Is(err, appgroup.ErrGroupNotFound) {
		t.Fatalf("Create missing source error = %v, want ErrGroupNotFound", err)
	}
	if _, err := NewGroupStore(db).Create(ctx, appgroup.CreateInput{
		Name: "Mismatch", Platform: "openai", CopyAccountsFromGroupIDs: []int{otherPlatformGroup.ID},
	}); !errors.Is(err, appgroup.ErrSourceGroupPlatformMismatch) {
		t.Fatalf("Create mismatched source error = %v, want ErrSourceGroupPlatformMismatch", err)
	}

	name := "Updated"
	newRate := 3.5
	isExclusive := false
	statusVisible := false
	subscriptionType := "standard"
	modelPolicy := modelpolicy.Policy{Deny: []string{"o3*"}}
	dispatchDSL := sdk.DispatchDSL{Rules: []sdk.DispatchRule{{ID: "rule-2", Operation: "responses"}}}
	serviceTier := "standard"
	forceInstructions := "updated instructions"
	note := "updated note"
	sortWeight := 9
	updated, err := NewGroupStore(db).Update(ctx, created.ID, appgroup.UpdateInput{
		Name:                     &name,
		RateMultiplier:           &newRate,
		IsExclusive:              &isExclusive,
		StatusVisible:            &statusVisible,
		SubscriptionType:         &subscriptionType,
		Quotas:                   map[string]any{"monthly": float64(500)},
		ModelRouting:             map[string][]int64{"o3": {int64(sourceAccount.ID)}},
		ModelPolicy:              &modelPolicy,
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{"apikey": {Allow: []string{"o3"}}},
		DispatchDSL:              &dispatchDSL,
		OperationPolicies:        map[string]bool{"images.edit": true},
		PluginSettings:           map[string]map[string]string{"claude": {"code": "true"}},
		ServiceTier:              &serviceTier,
		ForceInstructions:        &forceInstructions,
		Note:                     &note,
		SortWeight:               &sortWeight,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Name != name || updated.RateMultiplier != newRate || updated.StatusVisible || updated.ServiceTier != serviceTier ||
		updated.ForceInstructions != forceInstructions || updated.Note != note || updated.SortWeight != sortWeight {
		t.Fatalf("updated group missing scalar fields: %+v", updated)
	}
	if updated.Quotas["monthly"] != float64(500) || updated.ModelRouting["o3"][0] != int64(sourceAccount.ID) {
		t.Fatalf("updated group missing config fields: %+v", updated)
	}
	if _, err := NewGroupStore(db).Update(ctx, 999999, appgroup.UpdateInput{Name: &name}); !errors.Is(err, appgroup.ErrGroupNotFound) {
		t.Fatalf("Update missing error = %v, want ErrGroupNotFound", err)
	}
}

func TestGroupStoreCreateWithoutCopyPersistsConfig(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	rate := 1.75
	created, err := NewGroupStore(db).Create(ctx, appgroup.CreateInput{
		Name:                     "No Copy",
		Platform:                 "openai",
		RateMultiplier:           &rate,
		StatusVisible:            true,
		SubscriptionType:         "standard",
		Quotas:                   map[string]any{"daily": float64(50)},
		ModelRouting:             map[string][]int64{"gpt-5": {1, 2}},
		ModelPolicy:              modelpolicy.Policy{Allow: []string{"gpt-5"}},
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{"apikey": {Deny: []string{"o3"}}},
		DispatchDSL:              sdk.DispatchDSL{Rules: []sdk.DispatchRule{{ID: "fast", Operation: "chat"}}},
		OperationPolicies:        map[string]bool{"responses.create": true},
		PluginSettings:           map[string]map[string]string{"openai": {"mode": "fast"}},
		ServiceTier:              "standard",
		ForceInstructions:        "force",
		Note:                     "note",
		SortWeight:               7,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.Name != "No Copy" || created.RateMultiplier != rate || created.Quotas["daily"] != float64(50) ||
		created.ModelRouting["gpt-5"][1] != int64(2) || created.PluginSettings["openai"]["mode"] != "fast" {
		t.Fatalf("created group = %+v", created)
	}

	if _, err := NewGroupStore(db).Create(ctx, appgroup.CreateInput{Name: "", Platform: "openai"}); err == nil {
		t.Fatal("Create invalid no-copy group returned nil error")
	}
}

func TestGroupStoreDeleteClearsOptionalEdgesAndProtectsSubscriptions(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "group-delete@example.com")
	subscribed, err := db.Group.Create().SetName("Subscribed").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create subscribed group: %v", err)
	}
	if _, err := db.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(subscribed.ID).
		SetEffectiveAt(time.Now().Add(-time.Hour)).
		SetExpiresAt(time.Now().Add(time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if err := NewGroupStore(db).Delete(ctx, subscribed.ID); !errors.Is(err, appgroup.ErrGroupHasSubscriptions) {
		t.Fatalf("Delete subscribed error = %v, want ErrGroupHasSubscriptions", err)
	}

	deletable, err := db.Group.Create().SetName("Deletable").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create deletable group: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("group-key").
		SetKeyHash("hash-group-key").
		SetUserID(user.ID).
		SetGroupID(deletable.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	log, err := db.UsageLog.Create().
		SetBillingEventID("bill_group_delete").
		SetPlatform("openai").
		SetModel("gpt-5").
		SetGroupID(deletable.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create usage log: %v", err)
	}

	if err := NewGroupStore(db).Delete(ctx, deletable.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if exists, err := db.Group.Query().Where().Exist(ctx); err != nil || !exists {
		t.Fatalf("group query after delete exists=%v err=%v; subscribed group should remain", exists, err)
	}
	if hasGroup, err := db.APIKey.GetX(ctx, key.ID).QueryGroup().Exist(ctx); err != nil || hasGroup {
		t.Fatalf("api key has group=%v err=%v; want cleared group", hasGroup, err)
	}
	if hasGroup, err := db.UsageLog.GetX(ctx, log.ID).QueryGroup().Exist(ctx); err != nil || hasGroup {
		t.Fatalf("usage log has group=%v err=%v; want cleared group", hasGroup, err)
	}
	if err := NewGroupStore(db).Delete(ctx, 999999); !errors.Is(err, appgroup.ErrGroupNotFound) {
		t.Fatalf("Delete missing error = %v, want ErrGroupNotFound", err)
	}
}

func TestGroupStoreStatsForGroupsAggregatesAccountsAndUsage(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	group, err := db.Group.Create().SetName("Stats").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	createAccount := func(name string, state entaccount.State, maxConcurrency int, errorMsg string) int {
		t.Helper()
		builder := db.Account.Create().
			SetName(name).
			SetPlatform("openai").
			SetType("apikey").
			SetCredentials(map[string]string{"api_key": name}).
			SetState(state).
			SetMaxConcurrency(maxConcurrency).
			AddGroupIDs(group.ID)
		if errorMsg != "" {
			builder = builder.SetErrorMsg(errorMsg)
		}
		item, err := builder.Save(ctx)
		if err != nil {
			t.Fatalf("create account %q: %v", name, err)
		}
		return item.ID
	}
	activeID := createAccount("active", entaccount.StateActive, 3, "")
	createAccount("limited", entaccount.StateRateLimited, 5, "")
	createAccount("degraded", entaccount.StateDegraded, 7, "")
	deletedID := createAccount("disabled", entaccount.StateDisabled, 11, "")
	createAccount("manual-closed", entaccount.StateDisabled, 12, accountManualClosedReason)
	createAccount("error", entaccount.StateDisabled, 13, "bad credentials")
	if err := db.Account.UpdateOneID(deletedID).SetDeletedAt(time.Now()).Exec(ctx); err != nil {
		t.Fatalf("soft delete group account: %v", err)
	}

	todayStart := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		id        string
		createdAt time.Time
		totalCost float64
	}{
		{id: "today", createdAt: todayStart.Add(time.Hour), totalCost: 1.25},
		{id: "older", createdAt: todayStart.Add(-time.Hour), totalCost: 2.50},
	} {
		if _, err := db.UsageLog.Create().
			SetBillingEventID("bill_group_stats_" + item.id).
			SetPlatform("openai").
			SetModel("gpt-5").
			SetGroupID(group.ID).
			SetAccountID(activeID).
			SetTotalCost(item.totalCost).
			SetCreatedAt(item.createdAt).
			Save(ctx); err != nil {
			t.Fatalf("create usage log %q: %v", item.id, err)
		}
	}

	stats, capacities, err := NewGroupStore(db).StatsForGroups(ctx, []int{group.ID}, todayStart)
	if err != nil {
		t.Fatalf("StatsForGroups returned error: %v", err)
	}
	got := stats[group.ID]
	if got.AccountTotal != 5 || got.AccountActive != 3 || got.AccountDisabled != 1 || got.AccountError != 1 ||
		got.CapacityTotal != 15 || got.TodayCost != 1.25 || got.TotalCost != 3.75 {
		t.Fatalf("group stats = %+v", got)
	}
	if len(capacities[group.ID]) != 3 {
		t.Fatalf("active capacities = %+v, want active/rate_limited/degraded accounts", capacities[group.ID])
	}
	stats, capacities, err = NewGroupStore(db).StatsForGroups(ctx, nil, todayStart)
	if err != nil || stats != nil || capacities != nil {
		t.Fatalf("empty StatsForGroups = stats %+v capacities %+v err %v, want nil nil nil", stats, capacities, err)
	}
}

func TestGroupStoreCloneHelpers(t *testing.T) {
	if appgroupCloneQuotas(nil) != nil || appgroupCloneModelRouting(nil) != nil ||
		appgroupCloneAccountTypeModelPolicies(nil) != nil || appgroupCloneOperationPolicies(nil) != nil ||
		appgroupClonePluginSettings(nil) != nil {
		t.Fatal("nil clone helpers must return nil")
	}
	if got := appgroupCloneDispatchDSL(sdk.DispatchDSL{}); len(got.Rules) != 0 {
		t.Fatalf("empty dispatch DSL clone = %+v", got)
	}
	if got := groupRateMultiplierOrDefault(nil); got != 1 {
		t.Fatalf("default rate multiplier = %v, want 1", got)
	}

	quotas := map[string]any{"daily": float64(10)}
	quotasClone := appgroupCloneQuotas(quotas)
	quotasClone["daily"] = float64(20)
	if quotas["daily"] != float64(10) {
		t.Fatalf("quotas clone mutated source: %+v", quotas)
	}

	routing := map[string][]int64{"gpt": {1, 2}}
	routingClone := appgroupCloneModelRouting(routing)
	routingClone["gpt"][0] = 99
	if routing["gpt"][0] != 1 {
		t.Fatalf("model routing clone mutated source: %+v", routing)
	}

	policies := map[string]modelpolicy.Policy{"oauth": {Allow: []string{"gpt*"}}}
	policyClone := appgroupCloneAccountTypeModelPolicies(policies)
	policyClone["oauth"].Allow[0] = "mutated"
	if policies["oauth"].Allow[0] != "gpt*" {
		t.Fatalf("policy clone mutated source: %+v", policies)
	}

	dsl := sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		ID:         "rule",
		Candidates: []sdk.DispatchCandidate{{Scheduling: "gpt", Wire: "gpt-wire"}},
		When: sdk.DispatchWhen{
			Methods: []string{"POST"},
			Paths:   []string{"/v1"},
			Models:  []string{"gpt"},
		},
	}}}
	dslClone := appgroupCloneDispatchDSL(dsl)
	dslClone.Rules[0].Candidates[0].Scheduling = "mutated"
	dslClone.Rules[0].When.Methods[0] = "GET"
	if dsl.Rules[0].Candidates[0].Scheduling != "gpt" || dsl.Rules[0].When.Methods[0] != "POST" {
		t.Fatalf("dispatch clone mutated source: %+v", dsl)
	}

	ops := map[string]bool{"images.generate": true}
	opsClone := appgroupCloneOperationPolicies(ops)
	opsClone["images.generate"] = false
	if !ops["images.generate"] {
		t.Fatalf("operation policies clone mutated source: %+v", ops)
	}

	settings := map[string]map[string]string{"openai": {"images": "true"}}
	settingsClone := appgroupClonePluginSettings(settings)
	settingsClone["openai"]["images"] = "false"
	if settings["openai"]["images"] != "true" {
		t.Fatalf("plugin settings clone mutated source: %+v", settings)
	}

	value := 4.2
	if got := groupRateMultiplierOrDefault(&value); got != value {
		t.Fatalf("provided rate multiplier = %v, want %v", got, value)
	}
}
