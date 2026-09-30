package plantype

import (
	"strings"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type Filter struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	CredentialKey string   `json:"credential_key"`
	MatchMode     string   `json:"match"`
	Matches       []string `json:"matches,omitempty"`
}

// ResolveFilters consumes only the explicit SDK contract. Unknown is owned by
// Core; platforms without declarations have no inferred vendor-specific plans.
func ResolveFilters(plans []sdk.AccountPlan) []Filter {
	normalized, err := sdk.NormalizeAccountPlans(plans)
	if err != nil {
		normalized = nil
	} // Runtime publication rejects invalid contracts.
	result := make([]Filter, 0, len(normalized)+1)
	credentialKey := "plan_type"
	for _, plan := range normalized {
		credentialKey = plan.CredentialKey
		result = append(result, Filter{Key: plan.Key, Label: plan.Label, CredentialKey: plan.CredentialKey, MatchMode: string(plan.MatchMode), Matches: plan.Matches})
	}
	return append(result, Filter{Key: "unknown", Label: "Unknown", CredentialKey: credentialKey, MatchMode: "unknown"})
}
func (filter Filter) Known() bool {
	return filter.MatchMode != "unknown" && filter.MatchMode != "empty"
}

func (filter Filter) MatchesValue(value string) bool {
	if !filter.Known() {
		return false
	}
	for _, match := range filter.Matches {
		switch filter.MatchMode {
		case "contains":
			if strings.Contains(value, match) {
				return true
			}
		case "normalized_contains":
			if compact := Compact(match); compact != "" && strings.Contains(Compact(value), compact) {
				return true
			}
		default:
			if value == match {
				return true
			}
		}
	}
	return false
}
