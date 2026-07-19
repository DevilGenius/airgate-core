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
		Credential: &account.CredentialStringFilter{
			Platform:    "claude",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Claude Plus"},
			MatchMode:   "exact",
		},
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
		Credential: &account.CredentialStringFilter{
			Platform:    "kiro",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Pro"},
			MatchMode:   "contains",
		},
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
		Credential: &account.CredentialStringFilter{
			Platform:    "claude",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"Claude Plus"},
			MatchMode:   "exact",
		},
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
		Credential: &account.CredentialStringFilter{
			Platform:    "openai",
			AccountType: "oauth",
			Key:         "plan_type",
			Values:      []string{"free"},
			MatchMode:   "exact",
		},
	})
	if err != nil {
		t.Fatalf("ListAll returned error: %v", err)
	}
	if len(all) != 1 || all[0].Name != "OpenAI OAuth Free" {
		t.Fatalf("ListAll credential filter items = %+v, want only OpenAI OAuth Free", all)
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
