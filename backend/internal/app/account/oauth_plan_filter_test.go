package account

import (
	"testing"

	"github.com/DevilGenius/airgate-core/internal/plugin"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestUnknownOAuthPlanFilterUsesPlatformRules(t *testing.T) {
	for _, platform := range []string{"openai", "kiro", "claude"} {
		service := NewService(stubRepository{}, stubPluginCatalog{metas: []plugin.PluginMeta{{Platform: platform, AccountPlans: []sdk.AccountPlan{
			{Key: "plus", Matches: []string{"plus"}},
			{Key: "team", MatchMode: sdk.AccountPlanNormalizedContains, Matches: []string{"team", "k12", "prolite"}},
		}}}}, nil, nil)
		filter := service.normalizeListFilter(ListFilter{AccountType: oauthPlanFilterID(platform, "unknown") + ",apikey"})
		if filter.AccountType != "apikey" || len(filter.Credentials) != 1 {
			t.Fatalf("filter: %+v", filter)
		}
		unknown := filter.Credentials[0]
		if unknown.MatchMode != "unknown" || unknown.Platform != platform || len(unknown.KnownPlans) != 2 || len(unknown.KnownPlans[0].Values) != 1 {
			t.Fatalf("unknown: %+v", unknown)
		}
	}
}

func TestMetadataCannotDeclareAccountPlans(t *testing.T) {
	filters := pluginOAuthPlanFilters(plugin.PluginMeta{Platform: "claude", Metadata: map[string]string{"account.oauth_plans": "[{\"key\":\"max\"}]"}})
	if len(filters) != 1 || filters[0].Key != "unknown" || len(filters[0].KnownPlans) != 0 {
		t.Fatalf("metadata affected classification: %+v", filters)
	}
}
