package store

import (
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/predicate"
	entusagelog "github.com/DevilGenius/airgate-core/ent/usagelog"
	"github.com/DevilGenius/airgate-core/internal/pkg/timezone"
)

func usageTimeRange(dbDialect string, from, until time.Time) predicate.UsageLog {
	if dbDialect == dialect.Postgres {
		return entusagelog.And(entusagelog.CreatedAtGTE(from), entusagelog.CreatedAtLT(until))
	}
	return func(selector *sql.Selector) {
		column := "julianday(" + sqliteUsageTimestamp(selector.C(entusagelog.FieldCreatedAt)) + ")"
		selector.Where(sql.ExprP(column+" >= julianday(?) AND "+column+" < julianday(?)", from.UTC().Format(time.RFC3339Nano), until.UTC().Format(time.RFC3339Nano)))
	}
}

// SQL groups rows before they cross the driver boundary. PostgreSQL uses its
// timezone-aware bucketing; SQLite's bounded CASE also handles DST in tests.
func usageBucket(column, dbDialect, granularity string, from, until time.Time, loc *time.Location) string {
	if dbDialect == dialect.Postgres && loc.String() != "Local" {
		return "date_trunc(" + sqlStringLiteral(granularity) + ", " + column + " AT TIME ZONE " + sqlStringLiteral(loc.String()) + ")"
	}
	if dbDialect != dialect.Postgres {
		column = sqliteUsageTimestamp(column)
	}
	start := timezone.StartOfDay(from.In(loc))
	if granularity == "hour" {
		local := from.In(loc)
		start = local.Add(-time.Duration(local.Minute())*time.Minute - time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
	}
	var expression strings.Builder
	expression.WriteString("CASE")
	for index := 0; start.Before(until); index++ {
		next := start.AddDate(0, 0, 1)
		if granularity == "hour" {
			next = start.Add(time.Hour)
		}
		bound := sqlStringLiteral(next.UTC().Format(time.RFC3339Nano))
		if dbDialect == dialect.Postgres {
			fmt.Fprintf(&expression, " WHEN %s < %s::timestamptz THEN %d", column, bound, index)
		} else {
			fmt.Fprintf(&expression, " WHEN julianday(%s) < julianday(%s) THEN %d", column, bound, index)
		}
		start = next
	}
	expression.WriteString(" END")
	return expression.String()
}

func usageBucketTimestamp(dbDialect, granularity string, from, until time.Time, loc *time.Location, dimensions ...string) ent.AggregateFunc {
	return func(selector *sql.Selector) string {
		column := selector.C(entusagelog.FieldCreatedAt)
		groups := []string{usageBucket(column, dbDialect, granularity, from, until, loc)}
		for _, dimension := range dimensions {
			groups = append(groups, selector.C(dimension))
		}
		selector.GroupBy(groups...)
		if dbDialect == dialect.Postgres {
			return `to_char(MIN(` + column + `) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS created_at`
		}
		return `strftime('%Y-%m-%dT%H:%M:%fZ', ` + sqliteUsageTimestamp("MIN("+column+")") + `) AS created_at`
	}
}

// SQLite drivers may persist time.Time using Go's "... +0530 IST" format.
// Convert that offset to ISO-8601 without dropping its timezone or fractions.
func sqliteUsageTimestamp(column string) string {
	var expression strings.Builder
	expression.WriteString("CASE")
	for _, marker := range []string{" +", " -"} {
		position := "instr(" + column + ", " + sqlStringLiteral(marker) + ")"
		fmt.Fprintf(&expression, " WHEN %s > 0 THEN substr(%s, 1, %s-1) || substr(%s, %s+1, 3) || ':' || substr(%s, %s+4, 2)", position, column, position, column, position, column, position)
	}
	expression.WriteString(" ELSE " + column + " END")
	return expression.String()
}
