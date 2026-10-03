package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"

	"github.com/DevilGenius/airgate-core/ent"
	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
	"github.com/DevilGenius/airgate-core/internal/reporting"
)

// Reuse SQLite's row scanner while intercepting PostgreSQL-only projection queries.
type dashboardDistributionTestDriver struct {
	dialect.Driver
	query func(context.Context, string, any, any) error
}

func (d dashboardDistributionTestDriver) Dialect() string { return dialect.Postgres }

func (d dashboardDistributionTestDriver) Query(ctx context.Context, query string, args, result any) error {
	return d.query(ctx, query, args, result)
}

func TestDashboardDistributionReadsOnlyHourlyProjection(t *testing.T) {
	base := enttestOpen(t)
	defer func() { _ = base.Close() }()
	start := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	end := start.Add(12 * time.Hour)
	for _, userID := range []int{0, 42} {
		calls := 0
		driver := dashboardDistributionTestDriver{Driver: base.Driver()}
		driver.query = func(ctx context.Context, query string, args, result any) error {
			calls++
			if strings.Contains(query, "usage_rollup_coverage") {
				if !reflect.DeepEqual(args, []any{"usage_api_key_hourly_rollups"}) {
					t.Fatalf("coverage args = %#v", args)
				}
				return base.Driver().Query(ctx, "SELECT 1, 'ready', NULL", []any{}, result)
			}
			if query != dashboardDistributionQuery || !reflect.DeepEqual(args, []any{start, end, userID}) {
				t.Fatalf("unexpected query/args: %s, %#v", query, args)
			}
			if strings.Contains(query, "usage_logs") || strings.Count(query, "FROM public.usage_api_key_hourly_rollups") != 1 || !strings.Contains(query, "GROUP BY GROUPING SETS ((account_id), (group_id))") {
				t.Fatalf("query must aggregate both dimensions from hourly rollups only: %s", query)
			}
			return base.Driver().Query(ctx, `SELECT 'account', 31, 'shared name', 8, 123, 1.25, 2.5
UNION ALL SELECT 'account', 32, 'shared name', 2, 50, 0.5, 1
UNION ALL SELECT 'group', 7, '', 10, 173, 1.75, 3.5`, []any{}, result)
		}
		store := NewDashboardStore(ent.NewClient(ent.Driver(driver)))
		got, err := store.LoadDistributionStats(t.Context(), start, end, userID)
		want := appdashboard.DistributionSnapshot{
			Accounts: []appdashboard.DistributionStats{
				{ID: 31, Name: "shared name", Requests: 8, Tokens: 123, ActualCost: 1.25, StandardCost: 2.5},
				{ID: 32, Name: "shared name", Requests: 2, Tokens: 50, ActualCost: 0.5, StandardCost: 1},
			},
			Groups: []appdashboard.DistributionStats{{ID: 7, Requests: 10, Tokens: 173, ActualCost: 1.75, StandardCost: 3.5}},
		}
		if err != nil || !reflect.DeepEqual(got, want) || calls != 2 {
			t.Fatalf("distribution = %+v, err = %v, queries = %d", got, err, calls)
		}
	}
}

func TestDashboardDistributionDoesNotFallBackOnIncompleteHistory(t *testing.T) {
	base := enttestOpen(t)
	defer func() { _ = base.Close() }()
	driver := dashboardDistributionTestDriver{Driver: base.Driver()}
	driver.query = func(ctx context.Context, query string, args, result any) error {
		if !strings.Contains(query, "usage_rollup_coverage") {
			t.Fatalf("unexpected query after incomplete coverage: %s", query)
		}
		return base.Driver().Query(ctx, "SELECT 1, 'pending', NULL", []any{}, result)
	}
	store := NewDashboardStore(ent.NewClient(ent.Driver(driver)))
	if _, err := store.LoadDistributionStats(t.Context(), time.Now(), time.Now(), 0); !errors.Is(err, reporting.ErrHistoryNotReady) {
		t.Fatalf("error = %v, want history not ready", err)
	}
}

func TestDashboardDistributionPropagatesQueryError(t *testing.T) {
	base := enttestOpen(t)
	defer func() { _ = base.Close() }()
	wantErr := errors.New("projection query failed")
	driver := dashboardDistributionTestDriver{Driver: base.Driver()}
	driver.query = func(ctx context.Context, query string, args, result any) error {
		if strings.Contains(query, "usage_rollup_coverage") {
			return base.Driver().Query(ctx, "SELECT 1, 'ready', NULL", []any{}, result)
		}
		if query != dashboardDistributionQuery {
			t.Fatalf("unexpected fallback query: %s", query)
		}
		return wantErr
	}
	store := NewDashboardStore(ent.NewClient(ent.Driver(driver)))
	if _, err := store.LoadDistributionStats(t.Context(), time.Now(), time.Now(), 0); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want query error", err)
	}
}

func TestDashboardDistributionUnsupportedDialectReturnsEmptyWithoutScanning(t *testing.T) {
	base := enttestOpen(t)
	defer func() { _ = base.Close() }()
	got, err := NewDashboardStore(base).LoadDistributionStats(t.Context(), time.Now(), time.Now(), 0)
	if err != nil || got.Accounts == nil || got.Groups == nil || len(got.Accounts)+len(got.Groups) != 0 {
		t.Fatalf("distribution = %+v, err = %v", got, err)
	}
}
