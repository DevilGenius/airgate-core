package store

import (
	"testing"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
)

func TestAccountStoreUnknownOAuthPlanFilter(t *testing.T) {
	db := enttestOpen(t)
	defer db.Close()
	fixtures := []struct {
		name, platform, kind string
		credentials          map[string]string
		unknown              bool
	}{
		{"missing", "openai", "oauth", map[string]string{}, true},
		{"null", "openai", "oauth", nil, true},
		{"empty", "openai", "oauth", map[string]string{"plan_type": ""}, true},
		{"whitespace", "openai", "oauth", map[string]string{"plan_type": "  "}, true},
		{"custom", "openai", "oauth", map[string]string{"plan_type": "custom_subscription"}, true},
		{"free", "openai", "oauth", map[string]string{"plan_type": "free"}, false},
		{"plus", "openai", "oauth", map[string]string{"plan_type": "plus"}, false},
		{"pro", "openai", "oauth", map[string]string{"plan_type": "pro"}, false},
		{"team", "openai", "oauth", map[string]string{"plan_type": "team"}, false},
		{"k12", "openai", "oauth", map[string]string{"plan_type": "k12"}, false},
		{"prolite", "openai", "oauth", map[string]string{"plan_type": "Self_serve_business_prolite"}, false},
		{"apikey", "openai", "apikey", map[string]string{}, false},
		{"other-platform", "kiro", "oauth", map[string]string{}, false},
	}
	want := map[string]bool{}
	for _, fixture := range fixtures {
		db.Account.Create().SetName(fixture.name).SetPlatform(fixture.platform).SetType(fixture.kind).SetCredentials(fixture.credentials).SaveX(t.Context())
		if fixture.unknown {
			want[fixture.name] = true
		}
	}
	filter := appaccount.ListFilter{Page: 1, PageSize: 20, Credentials: []appaccount.CredentialStringFilter{{
		Platform: "openai", AccountType: "oauth", Key: "plan_type", MatchMode: "unknown",
		KnownPlans: []appaccount.CredentialStringFilter{
			{Key: "plan_type", MatchMode: "exact", Values: []string{"free", "plus", "pro"}},
			{Key: "plan_type", MatchMode: "normalized_contains", Values: []string{"team", "k12", "prolite"}},
		},
	}}}
	store := NewAccountStore(db)
	items, total, err := store.List(t.Context(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len(want)) || len(items) != len(want) {
		t.Fatalf("got %d/%d unknown accounts, want %d", total, len(items), len(want))
	}
	for _, item := range items {
		if !want[item.Name] {
			t.Fatalf("unexpected unknown account %q", item.Name)
		}
	}
	all, err := store.ListAll(t.Context(), filter)
	if err != nil || len(all) != len(want) {
		t.Fatalf("ListAll: count=%d err=%v", len(all), err)
	}
}
