package account

import (
	"encoding/json"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

func TestUnknownOAuthPlanFilterUsesDeclaredKnownRules(t *testing.T) {
	for _, key := range []string{"none", "unknown"} {
		t.Run(key, func(t *testing.T) {
			mode := "unknown"
			if key == "none" {
				mode = "empty"
			}
			raw, err := json.Marshal([]oauthPlanFilterMeta{
				{Key: key, Label: "None", MatchMode: mode},
				{Key: "plus", Matches: []string{"plus"}},
				{Key: "team", MatchMode: "normalized_contains", Matches: []string{"team", "k12", "prolite"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			service := NewService(stubRepository{}, stubPluginCatalog{metas: []plugin.PluginMeta{{Platform: "openai", Metadata: map[string]string{oauthPlanMetadataKey: string(raw)}}}}, nil, nil)
			filter := service.normalizeListFilter(ListFilter{AccountType: "oauth_plan:openai:unknown,apikey"})
			if filter.AccountType != "apikey" || len(filter.Credentials) != 1 {
				t.Fatalf("filter: %+v", filter)
			}
			unknown := filter.Credentials[0]
			if unknown.MatchMode != "unknown" || unknown.Platform != "openai" || unknown.AccountType != "oauth" || unknown.Key != "plan_type" || len(unknown.KnownPlans) != 2 {
				t.Fatalf("unknown filter: %+v", unknown)
			}
			if unknown.KnownPlans[1].MatchMode != "normalized_contains" || len(unknown.KnownPlans[1].Values) != 3 {
				t.Fatalf("lost known matching rules: %+v", unknown.KnownPlans)
			}
		})
	}
}
