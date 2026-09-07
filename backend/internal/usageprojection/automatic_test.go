package usageprojection

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestAutomaticMaintenanceRetriesUntilReady(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	calls := 0
	err := maintainUntilReady(ctx, func(context.Context) error {
		calls++
		if calls == 1 {
			return ErrVerificationBusy
		}
		if calls == 2 {
			return errors.New("temporary database outage")
		}
		return nil
	}, maintenancePolicy{attemptTimeout: time.Second, retryDelay: time.Millisecond, maxRetryDelay: 2 * time.Millisecond})
	if err != nil || calls != 3 {
		t.Fatalf("automatic retries = %d/%v", calls, err)
	}
}

func TestAutomaticMaintenanceContinuesAfterAttemptDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	calls := 0
	err := maintainUntilReady(ctx, func(attempt context.Context) error {
		calls++
		if calls == 1 {
			<-attempt.Done()
			return attempt.Err()
		}
		return nil
	}, maintenancePolicy{attemptTimeout: 5 * time.Millisecond, retryDelay: time.Millisecond, maxRetryDelay: time.Millisecond})
	if err != nil || calls != 2 {
		t.Fatalf("deadline resume = %d/%v", calls, err)
	}
}

func TestAutomaticMaintenanceStopsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	err := maintainUntilReady(ctx, func(context.Context) error { calls++; cancel(); return errors.New("interrupted") }, maintenancePolicy{attemptTimeout: time.Second, retryDelay: time.Hour, maxRetryDelay: time.Hour})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("shutdown = %d/%v", calls, err)
	}
}

func TestLocalAutomaticMaintenance(t *testing.T) {
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG for isolated PostgreSQL automatic maintenance tests")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local configuration failed")
	}
	for _, tc := range []struct {
		name             string
		facts, projected int
	}{{"empty_projection", 4, 0}, {"partial_projection", 4, 1}, {"verify_timeout", 4, 1}, {"fresh_database", 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("postgres", cfg.Database.DSN())
			if err != nil {
				t.Fatal("open local database failed")
			}
			defer db.Close()
			db.SetMaxOpenConns(4)
			schema := "ag_review_auto_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
			qualified := func(name string) string { return pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(name) }
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if !strings.HasPrefix(schema, "ag_review_auto_") {
					t.Error("unsafe cleanup schema")
					return
				}
				if _, err := db.ExecContext(cleanup, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE"); err != nil {
					t.Error(err)
				}
			}()
			for _, name := range []string{"usage_logs", "usage_hourly_rollups", "usage_api_key_hourly_rollups", "usage_rollup_coverage"} {
				exec("CREATE TABLE " + qualified(name) + " (LIKE public." + name + " INCLUDING ALL)")
			}
			for _, name := range []string{"api_keys", "groups", "accounts"} {
				exec("CREATE TABLE " + qualified(name) + " (id integer PRIMARY KEY)")
			}
			exec("INSERT INTO " + qualified("api_keys") + " VALUES(99)")
			exec("INSERT INTO " + qualified("usage_rollup_coverage") + "(projection) VALUES('usage_hourly_rollups'),('usage_api_key_hourly_rollups')")
			exec("INSERT INTO "+qualified("usage_logs")+`(id,billing_event_id,created_at,platform,model,user_id_snapshot,user_usage_logs,api_key_usage_logs,input_tokens,output_tokens,duration_ms,first_event_ms,first_token_ms,actual_cost,total_cost,billed_cost) SELECT id,'auto-'||id,'2026-01-01 00:15:00+00','openai','gpt-5',7,7,99,10,20,500,30,50,1.5,2,2.5 FROM generate_series(1,$1) id`, tc.facts)
			if tc.projected > 0 {
				tx, err := db.BeginTx(t.Context(), nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.ExecContext(t.Context(), "SET LOCAL search_path TO "+pq.QuoteIdentifier(schema)+", pg_catalog"); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				for _, p := range projections() {
					if _, err = tx.ExecContext(t.Context(), "WITH batch AS (SELECT "+sourceColumns+" FROM usage_logs WHERE id<=$1) "+p.upsertSQL(p.name), tc.projected); err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			options := RebuildOptions{Schema: schema, BatchSize: 2, VerifyTimeout: 5 * time.Second}
			if tc.name == "verify_timeout" {
				short := options
				short.VerifyTimeout = time.Nanosecond
				err := reconcileMaintenance(t.Context(), db, short)
				var pgErr *pq.Error
				timedOut := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &pgErr) && pgErr.Code == "57014")
				if !timedOut {
					t.Fatalf("verification deadline = %v", err)
				}
				state, err := inspectMaintenance(t.Context(), db, schema)
				if err != nil || !state.active {
					t.Fatalf("verification timeout did not advance into a resumable job: %+v/%v", state, err)
				}
			}
			if err := reconcileMaintenance(t.Context(), db, options); err != nil {
				t.Fatal(err)
			}
			state, err := inspectMaintenance(t.Context(), db, schema)
			if err != nil || state.active || state.pending || state.failed {
				t.Fatalf("automatic completion = %+v/%v", state, err)
			}
			for _, p := range projections() {
				var count int
				if err := db.QueryRowContext(t.Context(), "SELECT COALESCE(SUM(requests),0) FROM "+qualified(p.name)).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != tc.facts {
					t.Fatalf("%s count=%d, want %d", p.name, count, tc.facts)
				}
			}
			// A verified restart must not scan usage or attempt a new table swap.
			blocker, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err = blocker.ExecContext(t.Context(), "LOCK TABLE "+qualified("usage_logs")+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			quick, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			err = reconcileMaintenance(quick, db, options)
			cancel()
			blocker.Rollback()
			if err != nil {
				t.Fatalf("verified restart touched history: %v", err)
			}
			if tc.facts == 0 {
				return
			}
			// Simulate an interrupted forced job while the serving tables remain ready.
			interrupted, stop := context.WithCancel(t.Context())
			var job string
			err = Rebuild(interrupted, db, RebuildOptions{Schema: schema, BatchSize: 2, Force: true, Progress: func(p Progress) {
				job = p.JobID
				if p.Cursor > 0 {
					stop()
				}
			}})
			stop()
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("interrupt = %v", err)
			}
			options.Progress = func(p Progress) {
				if p.JobID != job {
					t.Fatal("automatic restart abandoned the existing job")
				}
			}
			if err := reconcileMaintenance(t.Context(), db, options); err != nil {
				t.Fatal(err)
			}
			state, err = inspectMaintenance(t.Context(), db, schema)
			if err != nil || state.active || state.pending || state.failed {
				t.Fatalf("resume = %+v/%v", state, err)
			}
		})
	}
}
