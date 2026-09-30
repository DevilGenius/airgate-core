package store

import (
	"encoding/json"
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/plantype"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
)

func TestAccountStoreUnknownOAuthPlanFilter(t *testing.T) {
	raw, _ := json.Marshal([]plantype.Filter{
		{Key: "free", Matches: []string{"free"}}, {Key: "plus", Matches: []string{"plus"}}, {Key: "pro", Matches: []string{"pro"}},
		{Key: "team", MatchMode: "normalized_contains", Matches: []string{"team", "k12", "prolite"}},
	})
	routegraph.SetPlatformPlanMetadata("openai", string(raw))
	defer routegraph.SetPlatformPlanMetadata("openai", "")
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
		{"legacy-category", "openai", "oauth", map[string]string{"plan_type": "custom_subscription", "account_category": "plus"}, true},
		{"enterprise", "openai", "oauth", map[string]string{"plan_type": "enterprise"}, true},
		{"case-sensitive", "openai", "oauth", map[string]string{"plan_type": "Plus"}, true},
		{"prefixed-plus", "openai", "oauth", map[string]string{"plan_type": "ChatGPT Plus"}, true},
		{"free", "openai", "oauth", map[string]string{"plan_type": "free"}, false},
		{"plus", "openai", "oauth", map[string]string{"plan_type": "plus"}, false},
		{"known-with-oauth-alias", "openai", "oauth", map[string]string{"plan_type": "plus", "account_type": "oauth"}, false},
		{"pro", "openai", "oauth", map[string]string{"plan_type": "pro"}, false},
		{"team", "openai", "oauth", map[string]string{"plan_type": "team"}, false},
		{"k12", "openai", "oauth", map[string]string{"plan_type": "k12"}, false},
		{"prolite", "openai", "oauth", map[string]string{"plan_type": "Self_serve_business_prolite"}, false},
		{"apikey", "openai", "apikey", map[string]string{}, false},
		{"apikey-with-oauth-plan", "openai", "apikey", map[string]string{"plan_type": "oauth"}, false},
		{"other-platform", "kiro", "oauth", map[string]string{}, false},
	}
	want := map[string]bool{}
	group := &ent.Group{ID: 999, Platform: "openai", AccountTypeModelPolicies: map[string]modelpolicy.Policy{"oauth": {Deny: []string{"unknown-blocked"}}}}
	for _, fixture := range fixtures {
		item := db.Account.Create().SetName(fixture.name).SetPlatform(fixture.platform).SetType(fixture.kind).SetCredentials(fixture.credentials).SaveX(t.Context())
		if fixture.platform == "openai" {
			group.Edges.Accounts = append(group.Edges.Accounts, item)
		}
		if fixture.unknown {
			want[fixture.name] = true
		}
	}
	knownPlans := []appaccount.CredentialStringFilter{}
	for _, rule := range plantype.ParseFilters(string(raw)) {
		if !rule.Known() {
			continue
		}
		knownPlans = append(knownPlans, appaccount.CredentialStringFilter{Key: rule.CredentialKey, MatchMode: rule.MatchMode, Values: rule.Matches})
	}
	filter := appaccount.ListFilter{Page: 1, PageSize: 20, Credentials: []appaccount.CredentialStringFilter{{
		Platform: "openai", AccountType: "oauth", Key: "plan_type", MatchMode: "unknown",
		KnownPlans: knownPlans,
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
	allowGroup := &ent.Group{ID: 1000, Platform: "openai", AccountTypeModelPolicies: map[string]modelpolicy.Policy{"oauth": {Allow: []string{"unknown-allowed"}}}}
	allowGroup.Edges.Accounts = group.Edges.Accounts
	restore := routegraph.SetSnapshotForTesting([]*ent.Group{group, allowGroup})
	defer restore()
	for _, check := range []struct {
		id    int
		model string
	}{{group.ID, "unknown-blocked"}, {allowGroup.ID, "other-model"}} {
		allowed := map[int]bool{}
		for _, item := range routegraph.Group(check.id).AccountsForModel(check.model) {
			allowed[item.ID] = true
		}
		for _, item := range group.Edges.Accounts {
			if allowed[item.ID] == want[item.Name] {
				t.Fatalf("group %d and Unknown filter disagree for %q", check.id, item.Name)
			}
		}
	}
	if got := routegraph.Group(allowGroup.ID).AccountsForModel("unknown-allowed"); len(got) != len(group.Edges.Accounts) {
		t.Fatalf("allowed model: got %d accounts", len(got))
	}
}
