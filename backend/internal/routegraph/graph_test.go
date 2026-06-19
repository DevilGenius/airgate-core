package routegraph

import (
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
)

func TestAccountsForModelAppliesModelPolicies(t *testing.T) {
	plus := &ent.Account{
		ID:          1,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "ChatGPT Plus"},
		ModelPolicy: modelpolicy.Policy{Deny: []string{"gpt-4o"}},
	}
	pro := &ent.Account{
		ID:          2,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "Builder Id Pro"},
		Extra:       map[string]interface{}{},
	}
	professional := &ent.Account{
		ID:          4,
		Platform:    "openai",
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "Professional"},
		Extra:       map[string]interface{}{},
	}
	apiKey := &ent.Account{
		ID:       3,
		Platform: "openai",
		Type:     "apikey",
		Extra:    map[string]interface{}{},
	}
	group := &ent.Group{
		ID:          10,
		Platform:    "openai",
		ModelPolicy: modelpolicy.Policy{Deny: []string{"blocked-*"}},
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{
			"plus": {Deny: []string{"gpt-5*"}},
			"pro":  {Allow: []string{"gpt-5*"}},
		},
	}
	group.Edges.Accounts = []*ent.Account{plus, pro, apiKey, professional}
	restore := SetSnapshotForTesting([]*ent.Group{group})
	defer restore()

	node := Group(group.ID)
	if got := accountIDs(node.AccountsForModel("blocked-model")); len(got) != 0 {
		t.Fatalf("blocked model accounts = %v, want none", got)
	}
	if got := accountIDs(node.AccountsForModel("gpt-5.1")); !sameIDs(got, []int{2, 3, 4}) {
		t.Fatalf("gpt-5 accounts = %v, want [2 3 4]", got)
	}
	if got := accountIDs(node.AccountsForModel("gpt-4o")); !sameIDs(got, []int{3, 4}) {
		t.Fatalf("gpt-4o accounts = %v, want [3 4]", got)
	}
}

func accountIDs(accounts []*ent.Account) []int {
	out := make([]int, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, account.ID)
	}
	return out
}

func sameIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
