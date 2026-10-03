package handler

import (
	"encoding/json"
	"testing"

	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
)

func TestDashboardTrendMapsDistributions(t *testing.T) {
	resp := toDashboardTrendResp(appdashboard.Trend{
		AccountDistribution: []appdashboard.DistributionStats{{ID: 31, Name: "credential", Requests: 8, Tokens: 123, ActualCost: 1.25, StandardCost: 2.5}},
		GroupDistribution:   []appdashboard.DistributionStats{{ID: 7, Name: "group", Requests: 8, Tokens: 123, ActualCost: 1.25, StandardCost: 2.5}},
	})
	if len(resp.AccountDistribution) != 1 || len(resp.GroupDistribution) != 1 {
		t.Fatalf("distributions = %+v", resp)
	}
	account := resp.AccountDistribution[0]
	group := resp.GroupDistribution[0]
	if account.ID != 31 || account.Name != "credential" || account.Requests != 8 || account.Tokens != 123 || account.ActualCost != 1.25 || account.StandardCost != 2.5 || group.ID != 7 || group.Name != "group" {
		t.Fatalf("account = %+v, group = %+v", account, group)
	}

	raw, err := json.Marshal(toDashboardTrendResp(appdashboard.Trend{}))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"account_distribution", "group_distribution"} {
		if string(payload[key]) != "[]" {
			t.Fatalf("empty %s = %s, want []", key, payload[key])
		}
	}
}
