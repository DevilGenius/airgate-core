package plantype

import (
	"encoding/json"
	"testing"
)

func TestPlatformFilters(t *testing.T) {
	raw, _ := json.Marshal([]Filter{{Key: "power", MatchMode: "contains", Matches: []string{"Power"}}, {Key: "power", Matches: []string{"ignored"}}})
	filters := ParseFilters(string(raw))
	if len(filters) != 2 || filters[1].Key != "unknown" {
		t.Fatalf("filters: %+v", filters)
	}
	if !filters[0].MatchesValue("Builder Id Power") || filters[0].MatchesValue("plus") || filters[0].MatchesValue("power") {
		t.Fatal("declared matching semantics changed")
	}
	if empty := ParseFilters("[]"); len(empty) != 1 || empty[0].Key != "unknown" {
		t.Fatalf("explicit empty declarations: %+v", empty)
	}
	for _, raw := range []string{"", "invalid"} {
		defaults := ParseFilters(raw)
		if len(defaults) < 2 || defaults[len(defaults)-1].Key != "unknown" {
			t.Fatalf("missing default rules: %+v", defaults)
		}
	}
}
