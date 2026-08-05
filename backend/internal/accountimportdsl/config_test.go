package accountimportdsl

import (
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

func TestApplyRejectsSequenceOverflow(t *testing.T) {
	config := Config{
		Version: Version,
		Rules: []Rule{{
			Name: "overflow",
			Set: Assignment{Priority: &PriorityAssignment{
				Mode: "sequence", Initial: intPtr(PriorityMax), Step: intPtr(1), GroupSize: intPtr(1),
			}},
		}},
	}
	_, err := config.Apply([]appaccount.CreateInput{{Name: "one"}, {Name: "two"}})
	if err == nil {
		t.Fatal("Apply() error = nil")
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
	if _, err := config.Apply([]appaccount.CreateInput{{Name: "one"}, {Name: "two"}, {Name: "three"}}); err == nil {
		t.Fatal("Apply() outside configured bounds error = nil")
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
