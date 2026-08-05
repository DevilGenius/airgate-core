package accountimportdsl

import (
	"fmt"
	"testing"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
)

func intPtr(value int) *int { return &value }

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
		`{"version":1,"rules":[{"name":"bad","when":[],"set":{"priority":{"mode":"sequence","initial":1,"step":0,"group_size":1}}}]}`,
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

func TestSequenceBoundaryIgnoresOccupiedPriority(t *testing.T) {
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
	got, err := config.ApplyWithOccupiedPriorities(
		[]appaccount.CreateInput{{Name: "one"}},
		[]int{100, 90},
	)
	if err != nil {
		t.Fatalf("ApplyWithOccupiedPriorities() error = %v", err)
	}
	if got[0].Priority != 90 {
		t.Fatalf("priority = %d, want occupied boundary 90", got[0].Priority)
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

func TestSequencePrioritySkipsOccupiedLevels(t *testing.T) {
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
	got, err := config.ApplyWithOccupiedPriorities(items, []int{8000, 7999})
	if err != nil {
		t.Fatalf("ApplyWithOccupiedPriorities() error = %v", err)
	}
	for index := 0; index < 5; index++ {
		if got[index].Priority != 7998 {
			t.Fatalf("priority[%d] = %d, want 7998", index, got[index].Priority)
		}
	}
	for index := 5; index < 7; index++ {
		if got[index].Priority != 7997 {
			t.Fatalf("priority[%d] = %d, want 7997", index, got[index].Priority)
		}
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
	got, err := config.ApplyWithOccupiedPriorities([]appaccount.CreateInput{{Name: "fixed"}}, []int{8000})
	if err != nil {
		t.Fatalf("ApplyWithOccupiedPriorities() error = %v", err)
	}
	if got[0].Priority != 8000 {
		t.Fatalf("fixed priority = %d, want 8000", got[0].Priority)
	}
}

func TestDisabledAssignmentsKeepValuesWithoutApplyingThem(t *testing.T) {
	disabled := false
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "disabled assignments",
			Set: Assignment{
				MaxConcurrency:        intPtr(99),
				MaxConcurrencyEnabled: &disabled,
				Priority:              &PriorityAssignment{Mode: "fixed", Value: intPtr(8000)},
				PriorityEnabled:       &disabled,
				GroupIDs:              []int64{2},
				GroupIDsEnabled:       &disabled,
			},
		}},
	}
	items, err := config.Apply([]appaccount.CreateInput{{
		Name: "one", MaxConcurrency: 10, Priority: 50, GroupIDs: []int64{1},
	}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if items[0].MaxConcurrency != 10 || items[0].Priority != 50 || len(items[0].GroupIDs) != 1 || items[0].GroupIDs[0] != 1 {
		t.Fatalf("disabled assignments changed item: %+v", items[0])
	}
}
