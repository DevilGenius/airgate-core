package handler

import (
	"encoding/json"
	"testing"

	apppluginadmin "github.com/DevilGenius/airgate-core/internal/app/pluginadmin"
	"github.com/DevilGenius/airgate-core/internal/plantype"
)

func TestPluginResponseProvidesPlatformPlanRules(t *testing.T) {
	custom, _ := json.Marshal([]plantype.Filter{{Key: "power", Label: "Power", MatchMode: "contains", Matches: []string{"Power"}}})
	for _, test := range []struct {
		platform, raw string
		count         int
	}{{"kiro", string(custom), 2}, {"claude", "", len(plantype.DefaultFilters()) + 1}} {
		metadata := map[string]string{plantype.FiltersMetadataKey: test.raw, "keep": "value"}
		resp := toPluginResp(apppluginadmin.PluginMeta{Platform: test.platform, Metadata: metadata})
		var filters []plantype.Filter
		if err := json.Unmarshal([]byte(resp.Metadata[plantype.FiltersMetadataKey]), &filters); err != nil {
			t.Fatal(err)
		}
		if len(filters) != test.count || filters[len(filters)-1].Key != "unknown" || resp.Metadata["keep"] != "value" {
			t.Fatalf("invalid platform metadata: %+v", filters)
		}
		if metadata[plantype.FiltersMetadataKey] != test.raw {
			t.Fatal("response mapping mutated plugin metadata")
		}
	}
}
