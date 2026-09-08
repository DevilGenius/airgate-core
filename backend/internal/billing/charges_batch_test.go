package billing

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/config"
)

func TestLocalBillingBatchLocksR06(t *testing.T) {
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG for isolated PostgreSQL billing tests")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local configuration failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(8)
	name := "ag_review_r06_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	quoted := pq.QuoteIdentifier(name)
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE SCHEMA " + quoted)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !strings.HasPrefix(name, "ag_review_r06_") {
			t.Error("unsafe cleanup schema")
			return
		}
		if _, err := db.ExecContext(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	exec("CREATE TABLE " + quoted + ".accounts(id int PRIMARY KEY,last_used_at timestamptz,updated_at timestamptz,deleted_at timestamptz)")
	exec("CREATE TABLE " + quoted + ".users(id int PRIMARY KEY,balance numeric(20,8),updated_at timestamptz)")
	exec("CREATE TABLE " + quoted + ".api_keys(id int PRIMARY KEY,used_quota float8,used_quota_actual float8,updated_at timestamptz)")
	exec("INSERT INTO " + quoted + ".accounts(id) VALUES(1),(2)")
	exec("INSERT INTO " + quoted + ".users(id,balance) VALUES(1,1000),(2,1000)")
	exec("INSERT INTO " + quoted + ".api_keys(id,used_quota,used_quota_actual) VALUES(1,0,0),(2,0,0)")
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	now := time.Now().UTC().Truncate(time.Microsecond)
	for worker := range 8 {
		workers.Go(func() {
			<-start
			for iteration := range 10 {
				tx, err := client.Tx(ctx)
				if err != nil {
					errors <- err
					return
				}
				err = execBillingUpdate(ctx, tx, "SET LOCAL search_path TO "+quoted+",pg_catalog", nil)
				if err == nil {
					err = execBillingUpdate(ctx, tx, "SET LOCAL lock_timeout='3s'", nil)
				}
				batch := []UsageRecord{{UserID: 1, APIKeyID: 1, ActualCost: 2, BilledCost: 3}, {UserID: 2, APIKeyID: 2, ActualCost: 2, BilledCost: 3}}
				if worker%2 == 1 {
					batch[0], batch[1] = batch[1], batch[0]
				}
				if err == nil {
					at := now.Add(time.Duration(iteration) * time.Second)
					err = updateAccountLastUsedAt(ctx, tx, []insertedUsageLog{{Record: UsageRecord{AccountID: batch[0].UserID}, CreatedAt: at}, {Record: UsageRecord{AccountID: batch[1].UserID}, CreatedAt: at}})
				}
				if err == nil {
					err = applyUsageCharges(ctx, tx, batch)
				}
				if err != nil {
					_ = tx.Rollback()
					errors <- err
					return
				}
				if err := tx.Commit(); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	for _, id := range []int{1, 2} {
		var balance, billed, actual float64
		var last time.Time
		query := fmt.Sprintf("SELECT u.balance,k.used_quota,k.used_quota_actual,a.last_used_at FROM %s.users u JOIN %s.api_keys k USING(id) JOIN %s.accounts a USING(id) WHERE u.id=$1", quoted, quoted, quoted)
		if err := db.QueryRowContext(ctx, query, id).Scan(&balance, &billed, &actual, &last); err != nil {
			t.Fatal(err)
		}
		if balance != 840 || billed != 240 || actual != 160 || !last.Equal(now.Add(9*time.Second)) {
			t.Fatalf("account %d: balance=%v billed=%v actual=%v last=%v", id, balance, billed, actual, last)
		}
	}
	// Missing entities must fail and roll back all charges, rather than silently
	// turning a successful UPDATE of zero rows into a settled usage event.
	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := execBillingUpdate(ctx, tx, "SET LOCAL search_path TO "+quoted+",pg_catalog", nil); err != nil {
		t.Fatal(err)
	}
	if err := applyUsageCharges(ctx, tx, []UsageRecord{{UserID: 1, APIKeyID: 999, ActualCost: 2}}); err == nil {
		t.Fatal("missing key must fail")
	}
}
