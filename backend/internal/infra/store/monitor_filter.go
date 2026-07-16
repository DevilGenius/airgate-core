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

func applyMonitorHTTPStatusFilter(query *ent.MonitorRequestEventQuery, raw string) *ent.MonitorRequestEventQuery {
	includeRanges, excludeRanges := parseMonitorHTTPStatusFilter(raw)
	if len(includeRanges) == 0 && len(excludeRanges) == 0 {
		if len(strings.Fields(raw)) == 0 {
			return query
		}
		// Only an empty expression means "all". A non-empty expression with no
		// valid terms must not silently broaden the result set.
		return query.Where(entmonitorrequestevent.HTTPStatusEQ(-1))
	}
	if len(includeRanges) > 0 {
		query = query.Where(entmonitorrequestevent.Or(monitorHTTPStatusPredicates(includeRanges)...))
	}
	if len(excludeRanges) == 0 {
		return query
	}

	excluded := entmonitorrequestevent.Or(monitorHTTPStatusPredicates(excludeRanges)...)
	if len(includeRanges) > 0 {
		return query.Where(entmonitorrequestevent.Not(excluded))
	}
	// SQL NOT does not match NULL. With exclusion-only filters, a missing status
	// did not match the excluded expression and therefore remains visible.
	return query.Where(entmonitorrequestevent.Or(
		entmonitorrequestevent.HTTPStatusIsNil(),
		entmonitorrequestevent.Not(excluded),
	))
}

func parseMonitorHTTPStatusFilter(raw string) (includeRanges, excludeRanges []monitorHTTPStatusRange) {
	for _, rawTerm := range strings.Fields(raw) {
		term := rawTerm
		excluded := strings.HasPrefix(term, "!")
		if excluded {
			term = strings.TrimPrefix(term, "!")
		}
		statusRange, ok := parseMonitorHTTPStatusRange(term)
		if !ok {
			continue
		}
		if excluded {
			excludeRanges = appendUniqueMonitorHTTPStatusRange(excludeRanges, statusRange)
		} else {
			includeRanges = appendUniqueMonitorHTTPStatusRange(includeRanges, statusRange)
		}
	}
	return includeRanges, excludeRanges
}

func parseMonitorHTTPStatusRange(raw string) (monitorHTTPStatusRange, bool) {
	term := strings.ToLower(strings.TrimSpace(raw))
	if len(term) == 3 {
		if code, err := strconv.Atoi(term); err == nil {
			if code >= monitorHTTPStatusMin && code <= monitorHTTPStatusMax {
				return monitorHTTPStatusRange{min: code, max: code}, true
			}
			return monitorHTTPStatusRange{}, false
		}
	}

	prefix := ""
	if strings.HasSuffix(term, "*") && len(term) <= 3 {
		prefix = strings.TrimRight(term, "*")
	} else if len(term) == 3 {
		wildcardAt := strings.IndexAny(term, "x*")
		if wildcardAt > 0 && strings.Trim(term[wildcardAt:], "x*") == "" {
			prefix = term[:wildcardAt]
		}
	}
	if prefix == "" || len(prefix) > 2 {
		return monitorHTTPStatusRange{}, false
	}
	for _, char := range prefix {
		if char < '0' || char > '9' {
			return monitorHTTPStatusRange{}, false
		}
	}

	prefixValue, err := strconv.Atoi(prefix)
	if err != nil {
		return monitorHTTPStatusRange{}, false
	}
	scale := 10
	if len(prefix) == 1 {
		scale = 100
	}
	minStatus := prefixValue * scale
	maxStatus := minStatus + scale - 1
	if minStatus < monitorHTTPStatusMin || maxStatus > monitorHTTPStatusMax {
		return monitorHTTPStatusRange{}, false
	}
	return monitorHTTPStatusRange{min: minStatus, max: maxStatus}, true
}

func appendUniqueMonitorHTTPStatusRange(ranges []monitorHTTPStatusRange, candidate monitorHTTPStatusRange) []monitorHTTPStatusRange {
	for _, statusRange := range ranges {
		if statusRange == candidate {
			return ranges
		}
	}
	return append(ranges, candidate)
}

func monitorHTTPStatusPredicates(ranges []monitorHTTPStatusRange) []predicate.MonitorRequestEvent {
	predicates := make([]predicate.MonitorRequestEvent, 0, len(ranges))
	for _, statusRange := range ranges {
		if statusRange.min == statusRange.max {
			predicates = append(predicates, entmonitorrequestevent.HTTPStatusEQ(statusRange.min))
			continue
		}
		predicates = append(predicates, entmonitorrequestevent.And(
			entmonitorrequestevent.HTTPStatusGTE(statusRange.min),
			entmonitorrequestevent.HTTPStatusLTE(statusRange.max),
		))
	}
	return predicates
}
