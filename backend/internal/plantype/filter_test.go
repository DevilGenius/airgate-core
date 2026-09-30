package plantype

import (
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestPlatformFilters(t *testing.T) {
	filters := ResolveFilters([]sdk.AccountPlan{{Key: "power", MatchMode: sdk.AccountPlanContains, Matches: []string{"Power"}}})
	if len(filters) != 2 || filters[1].Key != "unknown" {
		t.Fatalf("filters: %+v", filters)
	}
	if !filters[0].MatchesValue("Builder Id Power") || filters[0].MatchesValue("plus") || filters[0].MatchesValue("power") {
		t.Fatal("declared matching semantics changed")
	}
	for _, plans := range [][]sdk.AccountPlan{nil, {}, {{Key: "oauth"}}} {
		got := ResolveFilters(plans)
		if len(got) != 1 || got[0].Key != "unknown" {
			t.Fatalf("must not invent platform plans: %+v", got)
		}
	}
	alternate := ResolveFilters([]sdk.AccountPlan{{Key: "max", CredentialKey: "subscription"}})
	if alternate[1].CredentialKey != "subscription" {
		t.Fatal("Unknown must use the declared identity field")
	}
}
