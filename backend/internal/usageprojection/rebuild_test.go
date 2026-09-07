package usageprojection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestLocalRebuildResumesAndCapturesLateIDs(t *testing.T) {
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG for isolated PostgreSQL rebuild tests")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local configuration failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	schema := "ag_review_r08_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
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
		if !strings.HasPrefix(schema, "ag_review_r08_") {
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
	exec("INSERT INTO " + qualified("usage_rollup_coverage") + "(projection,state) VALUES('usage_hourly_rollups','ready'),('usage_api_key_hourly_rollups','ready')")
	columns := `(id,billing_event_id,created_at,platform,model,user_id_snapshot,user_usage_logs,api_key_usage_logs,input_tokens,output_tokens,duration_ms,first_event_ms,first_token_ms,actual_cost,total_cost,billed_cost)`
	exec("INSERT INTO " + qualified("usage_logs") + columns + ` SELECT id,'r08-'||id,'2026-01-01 00:15:00+00','openai','gpt-5',7,7,99,10,20,500,30,50,1.5,2,2.5 FROM generate_series(10,109) id`)
	seed, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = seed.ExecContext(t.Context(), "SET LOCAL search_path TO "+pq.QuoteIdentifier(schema)+", pg_catalog"); err != nil {
		seed.Rollback()
		t.Fatal(err)
	}
	for _, p := range projections() {
		if _, err = seed.ExecContext(t.Context(), "WITH batch AS (SELECT "+sourceColumns+" FROM usage_logs) "+p.upsertSQL(p.name)); err != nil {
			seed.Rollback()
			t.Fatal(err)
		}
	}
	if err = seed.Commit(); err != nil {
		t.Fatal(err)
	}
	write := func(id int) {
		t.Helper()
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(t.Context(), "SET LOCAL search_path TO "+pq.QuoteIdentifier(schema)+", pg_catalog"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(t.Context(), "INSERT INTO usage_logs"+columns+` VALUES($1,$2,'2026-01-01 00:15:00+00','openai','gpt-5',7,7,99,10,20,500,30,50,1.5,2,2.5)`, id, fmt.Sprintf("r08-%d", id)); err != nil {
			t.Fatal(err)
		}
		// Simulate the existing billing transaction's serving-projection writes.
		for _, p := range projections() {
			if _, err = tx.ExecContext(t.Context(), "WITH batch AS (SELECT "+sourceColumns+" FROM usage_logs WHERE id=$1) "+p.upsertSQL(p.name), id); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	count := func(name string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRowContext(t.Context(), "SELECT COALESCE(SUM(requests),0) FROM "+qualified(name)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s count=%d, want %d", name, got, want)
		}
	}
	runCtx, cancel := context.WithCancel(t.Context())
	var jobID string
	err = Rebuild(runCtx, db, RebuildOptions{Schema: schema, BatchSize: 25, Force: true, Progress: func(p Progress) {
		jobID = p.JobID
		if p.Cursor > 0 {
			write(1)
			write(1000)
			cancel()
		}
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted rebuild = %v", err)
	}
	count("usage_hourly_rollups", 102)
	count("usage_api_key_hourly_rollups", 102)
	var reader *sql.Tx
	defer func() {
		if reader != nil {
			reader.Rollback()
		}
	}()
	started := time.Now()
	err = Rebuild(t.Context(), db, RebuildOptions{Schema: schema, BatchSize: 25, Progress: func(p Progress) {
		if p.JobID != jobID {
			t.Fatal("resume created another job")
		}
		if p.State == "verified" {
			var err error
			reader, err = db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = reader.ExecContext(t.Context(), "SELECT 1 FROM "+qualified("usage_hourly_rollups")+" LIMIT 1"); err != nil {
				t.Fatal(err)
			}
		}
	}})
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("cutover with active reader = %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("cutover lock wait was not bounded")
	}
	write(1001)
	count("usage_hourly_rollups", 103)
	reader.Rollback()
	reader = nil
	if err = Rebuild(t.Context(), db, RebuildOptions{Schema: schema, BatchSize: 25}); err != nil {
		t.Fatal(err)
	}
	for _, p := range projections() {
		count(p.name, 103)
		count(backupName(p, jobID), 103)
	}
	write(1002)
	for _, p := range projections() {
		count(p.name, 104)
		count(backupName(p, jobID), 103)
	}
	var active int
	if err = db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+qualified("usage_rollup_rebuilds")+" WHERE state<>'ready'").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatal("finished job remained active")
	}
}
