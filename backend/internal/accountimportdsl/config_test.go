package accountimportdsl

import (
	"fmt"
	"testing"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
)

func intPtr(value int) *int { return &value }

func priorityCounts(values ...int) map[int]int {
	counts := make(map[int]int)
	for _, value := range values {
		counts[value]++
	}
	return counts
}

func TestApplyUsesFirstMatchingRuleAndIndependentSequences(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{
			{
				Name: "OpenAI Plus",
				When: []Condition{
					{Field: "platform", Op: "eq", Value: "openai"},
					{Field: "type", Op: "eq", Value: "oauth"},
					{Field: "credentials.plan_type", Op: "in", Values: []string{"plus"}},
				},
				Set: Assignment{
					MaxConcurrency: intPtr(20),
					Priority: &PriorityAssignment{
						Mode: "sequence", Initial: intPtr(1000), Step: intPtr(-10), GroupSize: intPtr(2),
					},
					GroupIDs: []int64{3, 5},
				},
			},
			{
				Name: "Catch all OAuth",
				When: []Condition{{Field: "type", Op: "eq", Value: "oauth"}},
				Set:  Assignment{Priority: &PriorityAssignment{Mode: "fixed", Value: intPtr(50)}},
			},
		},
	}
	items := []appaccount.CreateInput{
		{Name: "plus-1", Platform: "openai", Type: "oauth", Credentials: map[string]string{"plan_type": "Plus"}},
		{Name: "free", Platform: "openai", Type: "oauth", Credentials: map[string]string{"plan_type": "free"}},
		{Name: "plus-2", Platform: "openai", Type: "oauth", Credentials: map[string]string{"plan_type": "plus"}},
		{Name: "plus-3", Platform: "openai", Type: "oauth", Credentials: map[string]string{"plan_type": "plus"}},
	}

	got, err := config.Apply(items)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got[0].Priority != 1000 || got[2].Priority != 1000 || got[3].Priority != 990 {
		t.Fatalf("sequence priorities = %d, %d, %d", got[0].Priority, got[2].Priority, got[3].Priority)
	}
	if got[1].Priority != 50 {
		t.Fatalf("fallback priority = %d, want 50", got[1].Priority)
	}
	if got[0].MaxConcurrency != 20 || len(got[0].GroupIDs) != 2 || got[0].GroupIDs[0] != 3 {
		t.Fatalf("plus assignment = %+v", got[0])
	}
}

func TestParseRejectsInvalidPriorityAndUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"rules":[{"name":"bad","when":[],"set":{"priority":{"mode":"sequence","initial":1,"step":0,"group_size":1},"model_downgrade_threshold":0}}]}`,
		`{"version":1,"rules":[{"name":"bad","when":[],"set":null}]}`,
		`{"version":1,"rules":[{"name":"bad","when":[],"set":{}}]}`,
		`{"version":1,"rules":[{"name":"bad","when":[],"set":{"model_downgrade_threshold":0,"model_downgrade_threshold_enabled":true}}]}`,
		`{"version":1,"rules":[{"name":"bad","when":[],"set":{"model_downgrade_threshold":null}}]}`,
		`{"version":1,"rules":[{"name":"bad","when":[],"set":{"proxy_slot":"random","model_downgrade_threshold":0}}]}`,
		`{"version":1,"rules":[],"unknown":true}`,
		`{"version":2,"rules":[]}`,
	} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse(%s) error = nil", raw)
		}
	}
}

func TestApplyClampsSequenceOverflowToMaximum(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "overflow",
			Set: Assignment{Priority: &PriorityAssignment{
				Mode: "sequence", Initial: intPtr(PriorityMax), Step: intPtr(1), GroupSize: intPtr(1),
			}},
		}},
	}
	got, err := config.Apply([]appaccount.CreateInput{{Name: "one"}, {Name: "two"}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got[0].Priority != PriorityMax || got[1].Priority != PriorityMax {
		t.Fatalf("priorities = %d, %d, want %d, %d", got[0].Priority, got[1].Priority, PriorityMax, PriorityMax)
	}
}

func TestApplyHonorsSequenceBounds(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "bounded",
			Set: Assignment{Priority: &PriorityAssignment{
				Mode: "sequence", Initial: intPtr(100), Step: intPtr(-10), GroupSize: intPtr(1),
				Min: intPtr(90), Max: intPtr(100),
			}},
		}},
	}
	if _, err := config.Apply([]appaccount.CreateInput{{Name: "one"}, {Name: "two"}}); err != nil {
		t.Fatalf("Apply() within bounds error = %v", err)
	}
	got, err := config.Apply([]appaccount.CreateInput{{Name: "one"}, {Name: "two"}, {Name: "three"}, {Name: "four"}})
	if err != nil {
		t.Fatalf("Apply() outside configured bounds error = %v", err)
	}
	want := []int{100, 90, 90, 90}
	for index := range want {
		if got[index].Priority != want[index] {
			t.Fatalf("priority[%d] = %d, want %d", index, got[index].Priority, want[index])
		}
	}
}

func TestSequenceFillsPartiallyOccupiedPriorityBeforeAdvancing(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "bounded",
			Set: Assignment{Priority: &PriorityAssignment{
				Mode: "sequence", Initial: intPtr(100), Step: intPtr(-10), GroupSize: intPtr(2),
				Min: intPtr(90), Max: intPtr(100),
			}},
		}},
	}
	got, err := config.ApplyWithOccupiedPriorities(
		[]appaccount.CreateInput{{Name: "one"}, {Name: "two"}},
		priorityCounts(100),
	)
	if err != nil {
		t.Fatalf("ApplyWithOccupiedPriorities() error = %v", err)
	}
	if got[0].Priority != 100 || got[1].Priority != 90 {
		t.Fatalf("priorities = %d, %d, want 100, 90", got[0].Priority, got[1].Priority)
	}
}

func TestApplySkipsDisabledRule(t *testing.T) {
	disabled := false
	config := Config{
		Version: Version,
		Rules: []Rule{
			{Name: "disabled", Enabled: &disabled, Set: Assignment{MaxConcurrency: intPtr(99)}},
			{Name: "fallback", Set: Assignment{MaxConcurrency: intPtr(10)}},
		},
	}
	items, err := config.Apply([]appaccount.CreateInput{{Name: "one"}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if items[0].MaxConcurrency != 10 {
		t.Fatalf("max concurrency = %d, want fallback 10", items[0].MaxConcurrency)
	}
}

func TestSequencePriorityFillsOccupiedLevelsByGroupSize(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "sequence",
			Set: Assignment{Priority: &PriorityAssignment{
				Mode: "sequence", Initial: intPtr(8000), Step: intPtr(-1), GroupSize: intPtr(5),
			}},
		}},
	}
	items := make([]appaccount.CreateInput, 7)
	for index := range items {
		items[index].Name = fmt.Sprintf("account-%d", index)
	}
	got, err := config.ApplyWithOccupiedPriorities(items, priorityCounts(8000, 7999))
	if err != nil {
		t.Fatalf("ApplyWithOccupiedPriorities() error = %v", err)
	}
	for index := 0; index < 4; index++ {
		if got[index].Priority != 8000 {
			t.Fatalf("priority[%d] = %d, want 8000", index, got[index].Priority)
		}
	}
	for index := 4; index < 7; index++ {
		if got[index].Priority != 7999 {
			t.Fatalf("priority[%d] = %d, want 7999", index, got[index].Priority)
		}
	}
}

func TestSequencePriorityAdvancesPastFullOccupiedLevel(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "sequence",
			Set: Assignment{Priority: &PriorityAssignment{
				Mode: "sequence", Initial: intPtr(8000), Step: intPtr(-1), GroupSize: intPtr(2),
			}},
		}},
	}
	got, err := config.ApplyWithOccupiedPriorities(
		[]appaccount.CreateInput{{Name: "one"}},
		priorityCounts(8000, 8000, 8000),
	)
	if err != nil {
		t.Fatalf("ApplyWithOccupiedPriorities() error = %v", err)
	}
	if got[0].Priority != 7999 {
		t.Fatalf("priority = %d, want 7999", got[0].Priority)
	}
}

func TestFixedPriorityDoesNotSkipOccupiedValue(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "fixed",
			Set:  Assignment{Priority: &PriorityAssignment{Mode: "fixed", Value: intPtr(8000)}},
		}},
	}
	got, err := config.ApplyWithOccupiedPriorities([]appaccount.CreateInput{{Name: "fixed"}}, priorityCounts(8000))
	if err != nil {
		t.Fatalf("ApplyWithOccupiedPriorities() error = %v", err)
	}
	if got[0].Priority != 8000 {
		t.Fatalf("fixed priority = %d, want 8000", got[0].Priority)
	}
}

func TestParseAssignmentWithoutEnabledFields(t *testing.T) {
	config, err := Parse(`{"version":1,"rules":[{"name":"current","when":[],"set":{` +
		`"max_concurrency":99,` +
		`"priority":{"mode":"fixed","value":8000},` +
		`"group_ids":[2],` +
		`"proxy_id":7,"proxy_slot":"random",` +
		`"model_downgrade_threshold":0.8}}]}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assignment := config.Rules[0].Set
	if assignment.MaxConcurrency == nil || *assignment.MaxConcurrency != 99 ||
		assignment.Priority == nil || assignment.Priority.Value == nil || *assignment.Priority.Value != 8000 ||
		len(assignment.GroupIDs) != 1 || assignment.GroupIDs[0] != 2 ||
		assignment.ProxyID == nil || *assignment.ProxyID != 7 || assignment.ProxySlot == nil || !assignment.ProxySlot.Random ||
		assignment.ModelDowngradeThreshold != 0.8 {
		t.Fatalf("assignment = %+v", assignment)
	}
	items, err := config.Apply([]appaccount.CreateInput{{Name: "proxy"}})
	if err != nil || items[0].ProxyID == nil || *items[0].ProxyID != 7 || items[0].ProxyAssignment != appproxy.AssignmentRandom {
		t.Fatalf("applied proxy assignment = %+v, err=%v", items[0], err)
	}
}

func TestApplyAlwaysSetsModelDowngradeThreshold(t *testing.T) {
	config := Config{Version: Version, Rules: []Rule{{
		Name: "threshold",
		Set:  Assignment{ModelDowngradeThreshold: 0.85},
	}}}
	items, err := config.Apply([]appaccount.CreateInput{{Name: "one", ModelDowngradeThreshold: 0.2}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if items[0].ModelDowngradeThreshold != 0.85 {
		t.Fatalf("threshold = %v, want 0.85", items[0].ModelDowngradeThreshold)
	}
}

func TestProxySlotNumericAssignment(t *testing.T) {
	config, err := Parse(`{"version":1,"rules":[{"name":"proxy","when":[],"set":{` +
		`"proxy_id":7,"proxy_slot":42,"model_downgrade_threshold":0}}]}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	items, err := config.Apply([]appaccount.CreateInput{{Name: "numeric"}})
	if err != nil || items[0].ProxyID == nil || *items[0].ProxyID != 7 ||
		items[0].ProxyAssignment != appproxy.AssignmentCustom || items[0].ProxySlot == nil || *items[0].ProxySlot != 42 {
		t.Fatalf("numeric proxy assignment = %+v, err=%v", items[0], err)
	}
}
