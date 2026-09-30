package plantype

import (
	"encoding/json"
	"strings"
)

const FiltersMetadataKey = "account.oauth_plans"

type Filter struct {
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	CredentialKey string   `json:"credential_key"`
	MatchMode     string   `json:"match"`
	Matches       []string `json:"matches,omitempty"`
}

// DefaultFilters applies only when a platform has not declared plan metadata.
func DefaultFilters() []Filter {
	return []Filter{
		{Key: Free, Label: "Free", Matches: []string{Free}},
		{Key: Plus, Label: "Plus", Matches: []string{Plus}},
		{Key: Pro, Label: "Pro", Matches: []string{Pro}},
		{Key: Team, Label: "Team", MatchMode: "normalized_contains", Matches: []string{Team, K12, ProLite}},
		{Key: Enterprise, Label: "Enterprise", Matches: []string{Enterprise}},
	}
}

// ParseFilters normalizes platform declarations and supplies the common Unknown
// entry. Callers use the same definitions for SQL filters and cached route nodes.
func ParseFilters(raw string) []Filter {
	var declared []Filter
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &declared) != nil {
		declared = DefaultFilters()
	}
	result := make([]Filter, 0, len(declared)+1)
	seen := map[string]bool{}
	for _, item := range declared {
		item.Key = strings.TrimSpace(item.Key)
		if item.Key == "" {
			continue
		}
		item.CredentialKey = strings.TrimSpace(item.CredentialKey)
		if item.CredentialKey == "" {
			item.CredentialKey = "plan_type"
		}
		item.MatchMode = strings.ToLower(strings.TrimSpace(item.MatchMode))
		if item.Key == "none" && item.CredentialKey == "plan_type" && item.MatchMode == "empty" {
			item.Key, item.MatchMode = "unknown", "unknown"
		}
		if item.Key == "unknown" {
			item.MatchMode = "unknown"
		}
		switch item.MatchMode {
		case "contains", "normalized_contains", "empty", "unknown":
		default:
			item.MatchMode = "exact"
		}
		if item.MatchMode == "unknown" {
			item.Label = "Unknown"
		}
		item.Label = strings.TrimSpace(item.Label)
		if item.Label == "" {
			item.Label = item.Key
		}
		item.Matches = NormalizeMatches(item.Matches, item.Key)
		if item.MatchMode == "unknown" || item.MatchMode == "empty" {
			item.Matches = nil
		}
		if item.Known() && len(item.Matches) == 0 {
			continue
		}
		if seen[item.Key] {
			continue
		}
		seen[item.Key] = true
		result = append(result, item)
	}
	if !seen["unknown"] {
		result = append(result, Filter{Key: "unknown", Label: "Unknown", CredentialKey: "plan_type", MatchMode: "unknown"})
	}
	return result
}

func NormalizeMatches(values []string, fallback string) []string {
	if len(values) == 0 {
		values = []string{fallback}
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
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
