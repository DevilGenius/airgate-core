package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/ent/migrate"
	"github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestAccountStoreOccupiedPrioritiesGroupsAndExcludes(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	create := func(name string, priority int) int {
		item, err := db.Account.Create().
			SetName(name).
			SetPlatform("openai").
			SetType("oauth").
			SetCredentials(map[string]string{"access_token": name}).
			SetPriority(priority).
			Save(ctx)
		if err != nil {
			t.Fatalf("create account %s: %v", name, err)
		}
		return item.ID
	}
	excludedID := create("excluded", 100)
	create("same-priority", 100)
	if _, err := db.Account.Create().
		SetName("disabled").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "disabled"}).
		SetPriority(100).
		SetState(entaccount.StateDisabled).
		Save(ctx); err != nil {
		t.Fatalf("create disabled account: %v", err)
	}
	create("lower-priority", 90)

	store := NewAccountStore(db)
	counts, err := store.OccupiedPriorities(ctx, nil)
	if err != nil {
		t.Fatalf("OccupiedPriorities() error = %v", err)
	}
	if counts[100] != 3 || counts[90] != 1 || len(counts) != 2 {
		t.Fatalf("occupied counts = %v, want map[100:3 90:1]", counts)
	}

	counts, err = store.OccupiedPriorities(ctx, []int{excludedID})
	if err != nil {
		t.Fatalf("OccupiedPriorities(exclude) error = %v", err)
	}
	if counts[100] != 2 || counts[90] != 1 || len(counts) != 2 {
		t.Fatalf("excluded occupied counts = %v, want map[100:2 90:1]", counts)
	}

	enabledCounts, err := store.OccupiedEnabledPriorities(ctx)
	if err != nil {
		t.Fatalf("OccupiedEnabledPriorities() error = %v", err)
	}
	if enabledCounts[100] != 2 || enabledCounts[90] != 1 || len(enabledCounts) != 2 {
		t.Fatalf("enabled occupied counts = %v, want map[100:2 90:1]", enabledCounts)
	}
}

func TestAccountStoreObserveUsageGrowthAccumulatesResetsAndDays(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	item, err := db.Account.Create().
		SetName("usage-growth").
		SetPlatform("openai").
		SetType("oauth").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	store := NewAccountStore(db)
	observe := func(day string, five, seven *float64) {
		t.Helper()
		if err := store.ObserveUsageGrowth(ctx, item.ID, account.UsageGrowthObservation{
			Day: day, FiveHourPercent: five, SevenDayPercent: seven,
		}); err != nil {
			t.Fatalf("ObserveUsageGrowth(%s): %v", day, err)
		}
	}
	value := func(v float64) *float64 { return &v }

	observe("2026-08-24", value(10), value(20))
	observe("2026-08-24", value(70), value(36))
	observe("2026-08-24", value(5), value(36))

	got, err := db.Account.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got.Usage5hGrowthDate != "2026-08-24" || got.Usage5hDailyGrowth != 65 || got.Usage5hLastPercent != 5 {
		t.Fatalf("5h growth state = (%q, %v, %v), want (2026-08-24, 65, 5)", got.Usage5hGrowthDate, got.Usage5hDailyGrowth, got.Usage5hLastPercent)
	}
	if got.Usage7dGrowthDate != "2026-08-24" || got.Usage7dDailyGrowth != 16 || got.Usage7dLastPercent != 36 {
		t.Fatalf("7d growth state = (%q, %v, %v), want (2026-08-24, 16, 36)", got.Usage7dGrowthDate, got.Usage7dDailyGrowth, got.Usage7dLastPercent)
	}

	observe("2026-08-25", value(12), nil)
	got, err = db.Account.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get next-day account: %v", err)
	}
	if got.Usage5hGrowthDate != "2026-08-25" || got.Usage5hDailyGrowth != 0 || got.Usage5hLastPercent != 12 {
		t.Fatalf("next-day 5h state = (%q, %v, %v), want baseline (2026-08-25, 0, 12)", got.Usage5hGrowthDate, got.Usage5hDailyGrowth, got.Usage5hLastPercent)
	}
	if got.Usage7dGrowthDate != "2026-08-24" || got.Usage7dDailyGrowth != 16 {
		t.Fatalf("missing next-day 7d observation changed state: (%q, %v)", got.Usage7dGrowthDate, got.Usage7dDailyGrowth)
	}
}

func TestAccountStoreCreateRefreshesExistingOAuthByEmail(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	group, err := db.Group.Create().
		SetName("OAuth Group").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	proxy, err := db.Proxy.Create().
		SetName("OAuth Proxy").
		SetAddress("127.0.0.1").
		SetPort(8080).
		Save(ctx)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}

	store := NewAccountStore(db)
	email := "oauth@example.com"
	proxyID := int64(proxy.ID)
	rate := 2.5
	existing, err := store.Create(ctx, account.CreateInput{
		Name:           "Keep Custom Name",
		Email:          &email,
		Platform:       "openai",
		Type:           "oauth",
		Credentials:    map[string]string{"access_token": "old-access", "old_only": "remove-me"},
		Priority:       88,
		MaxConcurrency: 7,
		ProxyID:        &proxyID,
		RateMultiplier: &rate,
		GroupIDs:       []int64{int64(group.ID)},
		UpstreamIsPool: true,
		Extra:          map[string]any{"label": "keep"},
	})
	if err != nil {
		t.Fatalf("create existing OAuth account: %v", err)
	}
	stateUntil := time.Now().Add(time.Hour)
	if _, err := db.Account.UpdateOneID(existing.ID).
		SetState(entaccount.StateDisabled).
		SetStateUntil(stateUntil).
		SetErrorMsg("expired credentials").
		Save(ctx); err != nil {
		t.Fatalf("disable existing OAuth account: %v", err)
	}

	incomingEmail := " OAuth@Example.COM "
	incomingRate := 1.0
	refreshed, err := store.Create(ctx, account.CreateInput{
		Name:           "New OAuth Suggested Name",
		Email:          &incomingEmail,
		Platform:       "openai",
		Type:           "OAUTH",
		Credentials:    map[string]string{"access_token": "new-access", "refresh_token": "new-refresh"},
		Priority:       50,
		MaxConcurrency: 10,
		RateMultiplier: &incomingRate,
	})
	if err != nil {
		t.Fatalf("refresh existing OAuth account: %v", err)
	}
	if refreshed.ID != existing.ID || refreshed.Name != existing.Name || refreshed.Email == nil || *refreshed.Email != email {
		t.Fatalf("refreshed identity = %+v, want existing account identity", refreshed)
	}
	if refreshed.Credentials["access_token"] != "new-access" || refreshed.Credentials["refresh_token"] != "new-refresh" ||
		refreshed.Credentials["email"] != email {
		t.Fatalf("refreshed credentials = %+v", refreshed.Credentials)
	}
	if _, ok := refreshed.Credentials["old_only"]; ok {
		t.Fatalf("stale credentials were retained: %+v", refreshed.Credentials)
	}
	if refreshed.State != entaccount.StateActive.String() || refreshed.StateUntil != nil || refreshed.ErrorMsg != "" {
		t.Fatalf("refreshed state = %q until=%v error=%q", refreshed.State, refreshed.StateUntil, refreshed.ErrorMsg)
	}
	if refreshed.Priority != existing.Priority || refreshed.MaxConcurrency != existing.MaxConcurrency ||
		refreshed.RateMultiplier != existing.RateMultiplier || !refreshed.UpstreamIsPool ||
		len(refreshed.GroupIDs) != 1 || refreshed.GroupIDs[0] != int64(group.ID) ||
		refreshed.Proxy == nil || refreshed.Proxy.ID != proxy.ID || refreshed.Extra["label"] != "keep" {
		t.Fatalf("existing routing configuration was not preserved: %+v", refreshed)
	}
}

func TestAccountStoreCreateDoesNotOverwriteIncompatibleEmailAccount(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	store := NewAccountStore(db)
	email := "shared@example.com"
	if _, err := store.Create(ctx, account.CreateInput{
		Name: "OpenAI OAuth", Email: &email, Platform: "openai", Type: "oauth", Credentials: map[string]string{"access_token": "keep"},
	}); err != nil {
		t.Fatalf("create existing account: %v", err)
	}

	for _, input := range []account.CreateInput{
		{Name: "Claude OAuth", Email: &email, Platform: "claude", Type: "oauth", Credentials: map[string]string{"access_token": "claude"}},
		{Name: "OpenAI API Key", Email: &email, Platform: "openai", Type: "apikey", Credentials: map[string]string{"api_key": "sk-test"}},
	} {
		if _, err := store.Create(ctx, input); !errors.Is(err, account.ErrAccountEmailExists) {
			t.Fatalf("Create(%s/%s) error = %v, want ErrAccountEmailExists", input.Platform, input.Type, err)
		}
	}
}

func TestAccountStoreKeywordSearchMatchesOAuthEmail(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	if _, err := db.Account.Create().
		SetName("Claude Key").
		SetEmail("claude@example.com").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "token"}).
		Save(ctx); err != nil {
		t.Fatalf("create oauth account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("Other Key").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-test"}).
		Save(ctx); err != nil {
		t.Fatalf("create api key account: %v", err)
	}

	store := NewAccountStore(db)
	items, total, err := store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, Keyword: "CLAUDE@"})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(items) != 1 || items[0].Name != "Claude Key" {
		t.Fatalf("items = %+v", items)
	}
}

func TestAccountStoreCredentialStringFilterMatchesPluginDeclaredPlan(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	cases := []struct {
		name        string
		platform    string
		accountType string
		credentials map[string]string
	}{
		{name: "OpenAI OAuth Free", platform: "openai", accountType: "oauth", credentials: map[string]string{"plan_type": "free"}},
		{name: "Claude OAuth Plus", platform: "claude", accountType: "oauth", credentials: map[string]string{"plan_type": "Claude Plus"}},
		{name: "Kiro OAuth Pro", platform: "kiro", accountType: "oauth", credentials: map[string]string{"plan_type": "Builder Id Pro"}},
		{name: "Claude OAuth Unknown", platform: "claude", accountType: "oauth", credentials: map[string]string{}},
		{name: "Kiro API Key", platform: "kiro", accountType: "apikey", credentials: map[string]string{"plan_type": "Builder Id Plus"}},
	}
	for _, item := range cases {
		if _, err := db.Account.Create().
			SetName(item.name).
			SetPlatform(item.platform).
			SetType(item.accountType).
			SetCredentials(item.credentials).
			Save(ctx); err != nil {
			t.Fatalf("create account %q: %v", item.name, err)
		}
	}

	store := NewAccountStore(db)
	items, total, err := store.List(ctx, account.ListFilter{
		Page:     1,
		PageSize: 20,
		Credentials: []account.CredentialStringFilter{{
			Platform:    "claude",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Claude Plus"},
			MatchMode:   "exact",
		}},
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Claude OAuth Plus" || items[0].Platform != "claude" {
		t.Fatalf("exact credential filter items = %+v total = %d, want only Claude OAuth Plus", items, total)
	}

	items, total, err = store.List(ctx, account.ListFilter{
		Page:     1,
		PageSize: 20,
		Platform: "kiro",
		Credentials: []account.CredentialStringFilter{{
			Platform:    "kiro",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Pro"},
			MatchMode:   "contains",
		}},
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Kiro OAuth Pro" {
		t.Fatalf("contains credential filter items = %+v total = %d, want only Kiro OAuth Pro", items, total)
	}

	items, total, err = store.List(ctx, account.ListFilter{
		Page:     1,
		PageSize: 20,
		Platform: "openai",
		Credentials: []account.CredentialStringFilter{{
			Platform:    "claude",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Claude Plus"},
			MatchMode:   "exact",
		}},
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("conflicting platform filter items = %+v total = %d, want no matches", items, total)
	}

	items, total, err = store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, AccountType: "oauth"})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 4 || len(items) != 4 {
		t.Fatalf("oauth total = %d len = %d, want all four OAuth accounts", total, len(items))
	}

	all, err := store.ListAll(ctx, account.ListFilter{
		Credentials: []account.CredentialStringFilter{{
			Platform:    "openai",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"free"},
			MatchMode:   "exact",
		}},
	})
	if err != nil {
		t.Fatalf("ListAll returned error: %v", err)
	}
	if len(all) != 1 || all[0].Name != "OpenAI OAuth Free" {
		t.Fatalf("ListAll credential filter items = %+v, want only OpenAI OAuth Free", all)
	}

	items, total, err = store.List(ctx, account.ListFilter{
		Page:     1,
		PageSize: 20,
		Credentials: []account.CredentialStringFilter{{
			Platform:    "claude",
			AccountType: "oauth",
			Key:         "plan_type",
			MatchMode:   "empty",
		}},
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "Claude OAuth Unknown" {
		t.Fatalf("empty credential filter items = %+v total = %d, want only Claude OAuth Unknown", items, total)
	}
}

func TestAccountStoreListSortsByPriority(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	accounts := []struct {
		name     string
		priority int
	}{
		{name: "Low", priority: 10},
		{name: "High", priority: 90},
		{name: "Middle", priority: 50},
	}
	for _, item := range accounts {
		if _, err := db.Account.Create().
			SetName(item.name).
			SetPlatform("openai").
			SetType("apikey").
			SetCredentials(map[string]string{"api_key": item.name}).
			SetPriority(item.priority).
			Save(ctx); err != nil {
			t.Fatalf("create account %q: %v", item.name, err)
		}
	}

	store := NewAccountStore(db)
	desc, total, err := store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, SortBy: "priority", SortDir: "desc"})
	if err != nil {
		t.Fatalf("List desc returned error: %v", err)
	}
	if total != 3 {
		t.Fatalf("desc total = %d, want 3", total)
	}
	assertAccountNames(t, desc, []string{"High", "Middle", "Low"})

	asc, total, err := store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, SortBy: "priority", SortDir: "asc"})
	if err != nil {
		t.Fatalf("List asc returned error: %v", err)
	}
	if total != 3 {
		t.Fatalf("asc total = %d, want 3", total)
	}
	assertAccountNames(t, asc, []string{"Low", "Middle", "High"})
}

func assertAccountNames(t *testing.T, got []account.Account, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d: %+v", len(got), len(want), got)
	}
	for index, name := range want {
		if got[index].Name != name {
			t.Fatalf("got[%d].Name = %q, want %q; all = %+v", index, got[index].Name, name, got)
		}
	}
}

func enttestOpen(t *testing.T) *ent.Client {
	t.Helper()
	return testdb.OpenMemoryEnt(t, "account_store", migrate.WithGlobalUniqueID(false))
}
