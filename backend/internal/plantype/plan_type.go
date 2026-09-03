// Package plantype centralizes account plan identity and classification.
//
// A plan has three deliberately different views:
//   - Normalize preserves externally visible identities such as k12 and prolite.
//   - RoutingCategory folds team, k12, and prolite into the Team routing policy.
//   - EstimatePool keeps only plus in the Plus estimate path and routes every
//     other supported paid plan to the Pro estimate path.
package plantype

import "strings"

const (
	Free       = "free"
	Plus       = "plus"
	Pro        = "pro"
	Team       = "team"
	K12        = "k12"
	ProLite    = "prolite"
	Enterprise = "enterprise"
)

const (
	EstimatePoolPlus = "plus"
	EstimatePoolPro  = "pro"
)

// Tokens splits a plan description into lowercase ASCII letter/digit tokens.
func Tokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

// Compact removes separators from a plan description.
func Compact(value string) string {
	return strings.Join(Tokens(value), "")
}

// Normalize converts upstream plan descriptions into stable external names.
func Normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	tokens := Tokens(value)
	compact := strings.Join(tokens, "")
	// Check ProLite before the standalone "pro" token so pro_lite / pro lite
	// cannot be misclassified as Pro.
	if strings.HasSuffix(compact, ProLite) {
		return ProLite
	}
	for _, token := range tokens {
		switch token {
		case Free, Plus, Pro, Team, K12, Enterprise:
			return token
		case "professional":
			return Pro
		}
	}
	return strings.ToLower(value)
}

// RoutingCategory returns the account category used by group policies and
// failover-type comparisons.
func RoutingCategory(value string) string {
	plan := Normalize(value)
	switch plan {
	case Team, K12, ProLite:
		return Team
	default:
		return plan
	}
}

// EstimatePool returns the primary usage-estimate path for a normalized plan.
func EstimatePool(value string) string {
	switch Normalize(value) {
	case Plus:
		return EstimatePoolPlus
	case Pro, Team, K12, ProLite:
		return EstimatePoolPro
	default:
		return ""
	}
}
