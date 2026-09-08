package store

import (
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/DevilGenius/airgate-core/internal/reporting"
)

func TestRollupCoverageRequiresVerifiedHistory(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	exec := func(query string, args ...any) {
		t.Helper()
		var result sql.Result
		if err := db.Driver().Exec(t.Context(), query, args, &result); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE usage_rollup_coverage (projection TEXT PRIMARY KEY, version INTEGER, state TEXT, covered_from DATETIME)`)
	start := time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC)
	check := func(from time.Time, ready bool) {
		t.Helper()
		err := requireUsageRollupCoverage(t.Context(), db, usageAPIKeyHourlyRollupTable, from)
		if ready && err != nil {
			t.Fatal(err)
		}
		if !ready && !errors.Is(err, reporting.ErrHistoryNotReady) {
			t.Fatalf("unverified history error = %v", err)
		}
	}
	check(time.Time{}, false)
	exec(`INSERT INTO usage_rollup_coverage VALUES (?,1,'pending',?)`, "usage_api_key_hourly_rollups", start)
	check(time.Time{}, false)
	check(start.Add(-time.Hour), false)
	check(start, true)
	exec(`UPDATE usage_rollup_coverage SET state='failed'`)
	check(start, false)
	exec(`UPDATE usage_rollup_coverage SET state='ready'`)
	check(time.Time{}, true)
	exec(`UPDATE usage_rollup_coverage SET version=2`)
	check(time.Time{}, false)
}

func TestHourBoundaryUsesUTCProjectionAlignment(t *testing.T) {
	for _, tc := range []struct {
		zone    string
		aligned bool
	}{{"UTC", true}, {"America/New_York", true}, {"Asia/Kolkata", false}, {"Asia/Kathmandu", false}} {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Fatal(err)
		}
		if hourBoundary(time.Date(2026, 9, 8, 0, 0, 0, 0, loc)) != tc.aligned {
			t.Fatalf("unexpected bucket alignment for %s", tc.zone)
		}
	}
}
