package store

import (
	"strconv"
	"strings"

	"github.com/DevilGenius/airgate-core/ent"
	entmonitorrequestevent "github.com/DevilGenius/airgate-core/ent/monitorrequestevent"
	"github.com/DevilGenius/airgate-core/ent/predicate"
)

const (
	monitorHTTPStatusMin = 100
	monitorHTTPStatusMax = 599
)

type monitorHTTPStatusRange struct {
	min int
	max int
}

func splitMonitorFilterValues(raw string) []string {
	terms := strings.Fields(raw)
	if len(terms) < 2 {
		return terms
	}
	seen := make(map[string]struct{}, len(terms))
	values := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		values = append(values, term)
	}
	return values
}

// applyMonitorHTTPStatusFilter accepts both HTTP status expressions and error
// code terms because the admin UI exposes them through one search field.
func applyMonitorHTTPStatusFilter(query *ent.MonitorRequestEventQuery, raw string) *ent.MonitorRequestEventQuery {
	includeRanges, excludeRanges, includeErrorCodes, excludeErrorCodes := parseMonitorHTTPStatusAndErrorCodeFilter(raw)
	includePredicates := append(
		monitorHTTPStatusPredicates(includeRanges),
		monitorErrorCodePredicates(includeErrorCodes)...,
	)
	excludeStatusPredicates := monitorHTTPStatusPredicates(excludeRanges)
	excludeErrorPredicates := monitorErrorCodePredicates(excludeErrorCodes)

	if len(includePredicates) == 0 && len(excludeStatusPredicates) == 0 && len(excludeErrorPredicates) == 0 {
		if len(strings.Fields(raw)) == 0 {
			return query
		}
		// Only an empty expression means "all". A non-empty expression with no
		// usable terms must not silently broaden the result set.
		return query.Where(entmonitorrequestevent.HTTPStatusEQ(-1))
	}
	if len(includePredicates) > 0 {
		query = query.Where(entmonitorrequestevent.Or(includePredicates...))
	}
	if len(excludeStatusPredicates) > 0 {
		// SQL NOT does not match NULL. Keep rows without an HTTP status when
		// excluding status ranges, while checking both displayed status fields.
		query = query.Where(monitorHTTPStatusExclusionPredicate(excludeRanges))
	}
	if len(excludeErrorPredicates) > 0 {
		query = query.Where(entmonitorrequestevent.Not(entmonitorrequestevent.Or(excludeErrorPredicates...)))
	}
	return query
}

func parseMonitorHTTPStatusFilter(raw string) (includeRanges, excludeRanges []monitorHTTPStatusRange) {
	includeRanges, excludeRanges, _, _ = parseMonitorHTTPStatusAndErrorCodeFilter(raw)
	return includeRanges, excludeRanges
}

func parseMonitorHTTPStatusAndErrorCodeFilter(raw string) (
	includeRanges, excludeRanges []monitorHTTPStatusRange,
	includeErrorCodes, excludeErrorCodes []string,
) {
	for _, rawTerm := range strings.Fields(raw) {
		term := rawTerm
		excluded := strings.HasPrefix(term, "!")
		if excluded {
			term = strings.TrimPrefix(term, "!")
		}
		if term == "" {
			continue
		}
		statusRanges, ok := parseMonitorHTTPStatusPattern(term)
		if ok {
			for _, statusRange := range statusRanges {
				if excluded {
					excludeRanges = appendUniqueMonitorHTTPStatusRange(excludeRanges, statusRange)
				} else {
					includeRanges = appendUniqueMonitorHTTPStatusRange(includeRanges, statusRange)
				}
			}
			continue
		}
		if excluded {
			excludeErrorCodes = appendUniqueMonitorErrorCode(excludeErrorCodes, term)
		} else {
			includeErrorCodes = appendUniqueMonitorErrorCode(includeErrorCodes, term)
		}
	}
	return includeRanges, excludeRanges, includeErrorCodes, excludeErrorCodes
}

func parseMonitorHTTPStatusPattern(raw string) ([]monitorHTTPStatusRange, bool) {
	term := strings.TrimSpace(raw)
	if term == "" {
		return nil, false
	}
	for _, char := range term {
		if (char < '0' || char > '9') && char != '*' && char != '?' {
			return nil, false
		}
	}

	matches := make([]int, 0)
	for code := monitorHTTPStatusMin; code <= monitorHTTPStatusMax; code++ {
		if monitorWildcardMatch(term, strconv.Itoa(code)) {
			matches = append(matches, code)
		}
	}
	return compactMonitorHTTPStatusRanges(matches), true
}

func monitorWildcardMatch(pattern, value string) bool {
	patternIndex, valueIndex := 0, 0
	lastStarIndex, starMatchIndex := -1, 0
	for valueIndex < len(value) {
		if patternIndex < len(pattern) &&
			(pattern[patternIndex] == '?' || pattern[patternIndex] == value[valueIndex]) {
			patternIndex++
			valueIndex++
			continue
		}
		if patternIndex < len(pattern) && pattern[patternIndex] == '*' {
			lastStarIndex = patternIndex
			starMatchIndex = valueIndex
			patternIndex++
			continue
		}
		if lastStarIndex < 0 {
			return false
		}
		patternIndex = lastStarIndex + 1
		starMatchIndex++
		valueIndex = starMatchIndex
	}
	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}

func compactMonitorHTTPStatusRanges(values []int) []monitorHTTPStatusRange {
	if len(values) == 0 {
		// Keep a recognized pattern that matches no HTTP status as a false
		// predicate instead of treating it as an error-code search.
		return []monitorHTTPStatusRange{{min: 1, max: 0}}
	}
	ranges := make([]monitorHTTPStatusRange, 0, len(values))
	start, end := values[0], values[0]
	for _, value := range values[1:] {
		if value == end+1 {
			end = value
			continue
		}
		ranges = append(ranges, monitorHTTPStatusRange{min: start, max: end})
		start, end = value, value
	}
	return append(ranges, monitorHTTPStatusRange{min: start, max: end})
}

func appendUniqueMonitorHTTPStatusRange(ranges []monitorHTTPStatusRange, candidate monitorHTTPStatusRange) []monitorHTTPStatusRange {
	for _, statusRange := range ranges {
		if statusRange == candidate {
			return ranges
		}
	}
	return append(ranges, candidate)
}

func appendUniqueMonitorErrorCode(codes []string, candidate string) []string {
	for _, code := range codes {
		if code == candidate {
			return codes
		}
	}
	return append(codes, candidate)
}

func monitorHTTPStatusPredicates(ranges []monitorHTTPStatusRange) []predicate.MonitorRequestEvent {
	predicates := make([]predicate.MonitorRequestEvent, 0, len(ranges))
	for _, statusRange := range ranges {
		predicates = append(predicates, entmonitorrequestevent.Or(
			monitorHTTPStatusFieldPredicate(statusRange, false),
			monitorHTTPStatusFieldPredicate(statusRange, true),
		))
	}
	return predicates
}

func monitorHTTPStatusFieldPredicate(statusRange monitorHTTPStatusRange, upstream bool) predicate.MonitorRequestEvent {
	if statusRange.min == statusRange.max {
		if upstream {
			return entmonitorrequestevent.UpstreamStatusEQ(statusRange.min)
		}
		return entmonitorrequestevent.HTTPStatusEQ(statusRange.min)
	}
	if upstream {
		return entmonitorrequestevent.And(
			entmonitorrequestevent.UpstreamStatusGTE(statusRange.min),
			entmonitorrequestevent.UpstreamStatusLTE(statusRange.max),
		)
	}
	return entmonitorrequestevent.And(
		entmonitorrequestevent.HTTPStatusGTE(statusRange.min),
		entmonitorrequestevent.HTTPStatusLTE(statusRange.max),
	)
}

func monitorHTTPStatusExclusionPredicate(ranges []monitorHTTPStatusRange) predicate.MonitorRequestEvent {
	httpPredicates := make([]predicate.MonitorRequestEvent, 0, len(ranges))
	upstreamPredicates := make([]predicate.MonitorRequestEvent, 0, len(ranges))
	for _, statusRange := range ranges {
		httpPredicates = append(httpPredicates, monitorHTTPStatusFieldPredicate(statusRange, false))
		upstreamPredicates = append(upstreamPredicates, monitorHTTPStatusFieldPredicate(statusRange, true))
	}
	return entmonitorrequestevent.And(
		entmonitorrequestevent.Or(
			entmonitorrequestevent.HTTPStatusIsNil(),
			entmonitorrequestevent.Not(entmonitorrequestevent.Or(httpPredicates...)),
		),
		entmonitorrequestevent.Or(
			entmonitorrequestevent.UpstreamStatusIsNil(),
			entmonitorrequestevent.Not(entmonitorrequestevent.Or(upstreamPredicates...)),
		),
	)
}

func monitorErrorCodePredicates(codes []string) []predicate.MonitorRequestEvent {
	predicates := make([]predicate.MonitorRequestEvent, 0, len(codes))
	for _, code := range codes {
		predicates = append(predicates, entmonitorrequestevent.ErrorCodeContainsFold(code))
	}
	return predicates
}
