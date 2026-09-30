package handler

import (
	"testing"

	apppluginadmin "github.com/DevilGenius/airgate-core/internal/app/pluginadmin"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestPluginResponseProvidesPlatformPlanRules(t *testing.T) {
	for _, test := range []struct {
		platform string
		plans    []sdk.AccountPlan
		count    int
	}{
		{"kiro", []sdk.AccountPlan{{Key: "power", Label: "Power", MatchMode: sdk.AccountPlanContains, Matches: []string{"Power"}}}, 2},
		{"claude", nil, 1},
	} {
		metadata := map[string]string{"account.oauth_plans": "[{\"key\":\"max\"}]", "keep": "value"}
		resp := toPluginResp(apppluginadmin.PluginMeta{Platform: test.platform, AccountPlans: test.plans, Metadata: metadata})
		if len(resp.AccountPlans) != test.count || resp.AccountPlans[test.count-1].Key != "unknown" || resp.Metadata["keep"] != "value" {
			t.Fatalf("invalid contract response: %+v", resp)
		}
		if metadata["account.oauth_plans"] != resp.Metadata["account.oauth_plans"] {
			t.Fatal("metadata was repurposed")
		}
	}
}
