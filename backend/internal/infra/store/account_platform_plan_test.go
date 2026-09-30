package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/plantype"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
)

func TestUnknownPlanPoliciesAcrossPlatforms(t *testing.T) {
	for _, test := range []struct {
		platform, known string
		definitions     []plantype.Filter
	}{
		{"kiro", "Builder Id Power", []plantype.Filter{{Key: "power", MatchMode: "contains", Matches: []string{"Power"}}}},
		{"claude", "max_20x", []plantype.Filter{{Key: "max", Matches: []string{"max_20x", "max_5x"}}}},
		{"custom", "plus", nil},
	} {
		t.Run(test.platform, func(t *testing.T) {
			raw := ""
			if test.definitions != nil {
				encoded, _ := json.Marshal(test.definitions)
				raw = string(encoded)
			}
			routegraph.SetPlatformPlanMetadata(test.platform, raw)
			defer routegraph.SetPlatformPlanMetadata(test.platform, "")
			db := enttestOpen(t)
			defer db.Close()
			group := &ent.Group{ID: 123, Platform: test.platform, AccountTypeModelPolicies: map[string]modelpolicy.Policy{"oauth": {Deny: []string{"blocked"}}}}
			plans := []string{test.known, "", "unrecognized_plan"}
			if test.platform == "kiro" {
				plans = append(plans, strings.ToLower(test.known))
			}
			for _, plan := range plans {
				item := db.Account.Create().SetName("account-" + plan).SetPlatform(test.platform).SetType("oauth").SetCredentials(map[string]string{"plan_type": plan}).SaveX(t.Context())
				group.Edges.Accounts = append(group.Edges.Accounts, item)
			}
			restore := routegraph.SetSnapshotForTesting([]*ent.Group{group})
			defer restore()
			filter := appaccount.CredentialStringFilter{Platform: test.platform, AccountType: "oauth", Key: "plan_type", MatchMode: "unknown"}
			for _, known := range plantype.ParseFilters(raw) {
				if known.Known() {
					filter.KnownPlans = append(filter.KnownPlans, appaccount.CredentialStringFilter{Key: known.CredentialKey, MatchMode: known.MatchMode, Values: known.Matches})
				}
			}
			unknown, total, err := NewAccountStore(db).List(t.Context(), appaccount.ListFilter{Page: 1, PageSize: 20, Credentials: []appaccount.CredentialStringFilter{filter}})
			if err != nil || total != int64(len(plans)-1) {
				t.Fatalf("unknown query: total=%d err=%v", total, err)
			}
			allowed := routegraph.Group(group.ID).AccountsForModel("blocked")
			if len(allowed) != 1 || allowed[0].Credentials["plan_type"] != test.known {
				t.Fatalf("known platform plan misclassified: %+v", allowed)
			}
			for _, item := range unknown {
				if item.ID == allowed[0].ID {
					t.Fatal("group policy differs from account filter")
				}
			}
			// A plugin update must reclassify existing nodes without another DB refresh.
			routegraph.SetPlatformPlanMetadata(test.platform, "[]")
			if got := routegraph.Group(group.ID).AccountsForModel("blocked"); len(got) != 0 {
				t.Fatalf("stale plan cache after metadata change: %+v", got)
			}
			// Restore its declaration and verify cached nodes recover their known plan.
			routegraph.SetPlatformPlanMetadata(test.platform, raw)
			if got := routegraph.Group(group.ID).AccountsForModel("blocked"); len(got) != 1 {
				t.Fatalf("plan cache did not recover: %+v", got)
			}
		})
	}
}
