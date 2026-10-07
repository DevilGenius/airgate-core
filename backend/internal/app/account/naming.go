package account

import (
	"strings"

	"github.com/DevilGenius/airgate-core/internal/plantype"
)

// NormalizeCreateNaming defines the naming scope for every creation entry point.
// Only AutoName inputs request allocation in the write transaction.
func NormalizeCreateNaming(input CreateInput) CreateInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	if input.Type == "" {
		if strings.TrimSpace(input.Credentials["api_key"]) != "" {
			input.Type = "apikey"
		} else {
			input.Type = "oauth"
		}
	}
	return input
}

// AccountNamePlan uses the recognized plan identity with only its first letter
// capitalized. Missing and unrecognized plans share the Unknown counter.
func AccountNamePlan(raw string) string {
	plan := plantype.Normalize(raw)
	switch plan {
	case plantype.Free, plantype.Plus, plantype.Pro, plantype.Team,
		plantype.K12, plantype.ProLite, plantype.Enterprise:
		return strings.ToUpper(plan[:1]) + plan[1:]
	default:
		return "Unknown"
	}
}
