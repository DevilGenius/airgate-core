package bootstrap

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/lib/pq"
)

func TestSystemUpgradeLockRetryBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var attempts []time.Time
		err := retrySystemUpgradeLock(t.Context(), "fixture", func(context.Context) error {
			attempts = append(attempts, time.Now())
			if len(attempts) < 8 {
				return fmt.Errorf("wrapped driver error: %w", &pq.Error{Code: "55P03"})
			}
			return nil
		})
		if err != nil || len(attempts) != 8 {
			t.Fatalf("retry failed: attempts=%d err=%v", len(attempts), err)
		}
		want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
		for i, delay := range want {
			if got := attempts[i+1].Sub(attempts[i]); got != delay {
				t.Fatalf("backoff %d = %s, want %s", i, got, delay)
			}
		}
	})
}

func TestSystemUpgradeRetryClassifiesErrors(t *testing.T) {
	for _, code := range []pq.ErrorCode{"40P01", "42601", "42501", "57014"} {
		t.Run(string(code), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				failure := &pq.Error{Code: code}
				err := retrySystemUpgradeLock(t.Context(), "fixture", func(context.Context) error {
					calls++
					if calls == 1 {
						return failure
					}
					return nil
				})
				if code == "40P01" {
					if err != nil || calls != 2 {
						t.Fatalf("deadlock was not retried: %v / %d", err, calls)
					}
				} else if !errors.Is(err, failure) || calls != 1 {
					t.Fatalf("non-lock error was retried or lost: %v / %d", err, calls)
				}
			})
		})
	}
}

func TestSystemUpgradeLockWaitStopsOnCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		err := retrySystemUpgradeLock(ctx, "fixture", func(context.Context) error {
			calls++
			return &pq.Error{Code: "55P03"}
		})
		if !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("shutdown did not stop lock wait: %v / %d", err, calls)
		}
	})
}

func TestSystemUpgradeRetriesOnlyFailedStatement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		state := &systemUpgradeMockState{applied: map[string]*string{}, onExec: func(query string, _ []driver.NamedValue) error {
			if query == "SELECT 2" {
				calls++
				if calls == 1 {
					return &pq.Error{Code: "55P03"}
				}
			}
			return nil
		}}
		db := openSystemUpgradeMockDB(t, state)
		defer func() { _ = db.Close() }()
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err := executeSystemUpgradeSQL(t.Context(), conn, systemUpgrade{ID: "fixture", SQL: "SELECT 1; SELECT 2; SELECT 3;"}); err != nil {
			t.Fatal(err)
		}
		if state.execCount("SELECT 1") != 1 || state.execCount("SELECT 2") != 2 || state.execCount("SELECT 3") != 1 {
			t.Fatalf("successful statements replayed: %v", state.execs)
		}
	})
}

func TestRequestTraceDropWaitsForLockBeforeRecordingUpgrade(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const id = "20260928190000_persist_request_traces"
		state := &systemUpgradeMockState{applied: map[string]*string{}}
		for _, upgrade := range loadSystemUpgrades() {
			if upgrade.ID != id {
				checksum := upgrade.Checksum
				state.applied[upgrade.ID] = &checksum
			} else if maintenanceOnly(upgrade) {
				t.Fatal("required column removal was deferred to index maintenance")
			}
		}
		calls := 0
		started := time.Now()
		state.onExec = func(query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "DROP COLUMN IF EXISTS expires_at") {
				if _, recorded := state.applied[id]; recorded {
					t.Fatal("migration marked complete before DDL succeeded")
				}
				calls++
				// Cross the old two-minute startup deadline without exiting.
				if calls < 19 {
					return &pq.Error{Code: "55P03"}
				}
			}
			return nil
		}
		db := openSystemUpgradeMockDB(t, state)
		defer func() { _ = db.Close() }()
		if err := RunSystemUpgrades(t.Context(), entsql.OpenDB("postgres", db)); err != nil {
			t.Fatal(err)
		}
		if calls != 19 || time.Since(started) <= 2*time.Minute || state.applied[id] == nil {
			t.Fatalf("column removal did not finish after lock contention: calls=%d elapsed=%s", calls, time.Since(started))
		}
		if state.execCount("INSERT INTO public.system_upgrade") != 1 || !state.execContains("RESET lock_timeout") || !state.execContains("pg_advisory_unlock") {
			t.Fatal("migration completion or connection cleanup missing")
		}
	})
}

func TestRequestTraceDropFailureDoesNotRecordCompletion(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("shutdown=%t", shutdown), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const id = "20260928190000_persist_request_traces"
				state := &systemUpgradeMockState{applied: map[string]*string{}}
				for _, upgrade := range loadSystemUpgrades() {
					if upgrade.ID != id {
						checksum := upgrade.Checksum
						state.applied[upgrade.ID] = &checksum
					}
				}
				failure := &pq.Error{Code: "42501"}
				state.onExec = func(query string, _ []driver.NamedValue) error {
					if strings.Contains(query, "DROP COLUMN IF EXISTS expires_at") {
						if shutdown {
							return &pq.Error{Code: "55P03"}
						}
						return failure
					}
					return nil
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if shutdown {
					go func() {
						time.Sleep(100 * time.Millisecond)
						cancel()
					}()
				}
				db := openSystemUpgradeMockDB(t, state)
				defer func() { _ = db.Close() }()
				err := RunSystemUpgrades(ctx, entsql.OpenDB("postgres", db))
				if shutdown && !errors.Is(err, context.Canceled) || !shutdown && !errors.Is(err, failure) {
					t.Fatalf("unexpected migration error: %v", err)
				}
				if state.applied[id] != nil || state.execCount("INSERT INTO public.system_upgrade") != 0 || state.execCount("DROP COLUMN IF EXISTS expires_at") != 1 {
					t.Fatal("failed migration recorded completion or retried after shutdown")
				}
				if !state.execContains("RESET lock_timeout") || !state.execContains("pg_advisory_unlock") {
					t.Fatal("failed migration skipped connection cleanup")
				}
			})
		})
	}
}

func TestSystemUpgradeRetriesWholeExplicitTransaction(t *testing.T) {
	for _, code := range []pq.ErrorCode{"55P03", "40P01"} {
		t.Run(string(code), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				state := &systemUpgradeMockState{applied: map[string]*string{}, onExec: func(query string, _ []driver.NamedValue) error {
					if query == "SELECT 2" {
						calls++
						if calls == 1 {
							return &pq.Error{Code: code}
						}
					}
					return nil
				}}
				db := openSystemUpgradeMockDB(t, state)
				defer func() { _ = db.Close() }()
				conn, err := db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = conn.Close() }()
				source := "SELECT 0; BEGIN; SELECT 1; SELECT 2; COMMIT; SELECT 3;"
				if err := executeSystemUpgradeSQL(t.Context(), conn, systemUpgrade{ID: "fixture", SQL: source}); err != nil {
					t.Fatal(err)
				}
				want := []string{"SELECT 0", "BEGIN", "SELECT 1", "SELECT 2", "ROLLBACK", "BEGIN", "SELECT 1", "SELECT 2", "COMMIT", "SELECT 3"}
				if !reflect.DeepEqual(state.execs, want) {
					t.Fatalf("transaction retry crossed commit boundary: %v", state.execs)
				}
			})
		})
	}
}

func TestSystemUpgradeRollbackFailureStopsRetries(t *testing.T) {
	state := &systemUpgradeMockState{applied: map[string]*string{}, onExec: func(query string, _ []driver.NamedValue) error {
		switch query {
		case "SELECT 1":
			return &pq.Error{Code: "55P03"}
		case "ROLLBACK":
			return driver.ErrBadConn
		}
		return nil
	}}
	db := openSystemUpgradeMockDB(t, state)
	defer func() { _ = db.Close() }()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	err = executeSystemUpgradeSQL(t.Context(), conn, systemUpgrade{ID: "fixture", SQL: "BEGIN; SELECT 1; COMMIT;"})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") || state.execCount("BEGIN") != 1 || state.execCount("COMMIT") != 0 {
		t.Fatalf("unsafe retry after failed rollback: %v / %v", err, state.execs)
	}
}

func TestSystemUpgradeRejectsInvalidTransactionBoundaries(t *testing.T) {
	for _, source := range []string{"BEGIN; SELECT 1;", "COMMIT;", "BEGIN; BEGIN; COMMIT;", "BEGIN; SELECT 1; ROLLBACK;"} {
		if _, err := systemUpgradeSQLBatches(source); err == nil {
			t.Fatalf("unsafe transaction accepted: %s", source)
		}
	}
}
