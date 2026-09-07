package usageprojection

import (
	"database/sql"
	"os"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/config"
	_ "github.com/lib/pq"
)

// PostgreSQL temp tables shadow public tables on this private connection only.
// No public usage, balances, API keys or coverage states are modified.
func TestLocalCoverageVerification(t *testing.T) {
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG for isolated PostgreSQL verification")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load test config failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		t.Fatal("open test database failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"usage_logs", "usage_hourly_rollups", "usage_api_key_hourly_rollups", "usage_rollup_coverage"} {
		exec("CREATE TEMP TABLE " + table + " (LIKE public." + table + " INCLUDING ALL)")
	}
	for _, table := range []string{"api_keys", "groups", "accounts"} {
		exec("CREATE TEMP TABLE " + table + " (id integer PRIMARY KEY)")
	}
	exec(`INSERT INTO usage_rollup_coverage(projection) VALUES ('usage_hourly_rollups'),('usage_api_key_hourly_rollups')`)
	exec(`INSERT INTO usage_logs (id,billing_event_id,created_at,platform,model,user_id_snapshot,user_usage_logs,input_tokens,output_tokens,duration_ms,first_event_ms,first_token_ms,actual_cost,total_cost,billed_cost)
 VALUES (1,'r07-fixture','2026-01-01 00:15:00+00','openai','gpt-5',7,7,10,20,500,30,50,1.5,2,2.5)`)
	readyCount := func(want int) {
		t.Helper()
		var got int
		if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_rollup_coverage WHERE state='ready'").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("ready projections=%d, want %d", got, want)
		}
	}
	if err := VerifyPending(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	readyCount(0)
	exec(`INSERT INTO usage_hourly_rollups (bucket_start,user_id,model,requests,input_tokens,output_tokens,non_image_requests,non_image_duration_ms,first_event_requests,first_event_ms,first_token_requests,first_token_ms,actual_cost,total_cost)
 VALUES ('2026-01-01 00:00:00+00',7,'gpt-5',1,10,20,1,500,1,30,1,50,1.5,2)`)
	// Key 99 was deleted: the raw relation is null, but the historical ID remains.
	exec(`INSERT INTO usage_api_key_hourly_rollups (bucket_start,api_key_id,user_id,platform,model,requests,input_tokens,output_tokens,actual_cost,total_cost,billed_cost)
 VALUES ('2026-01-01 00:00:00+00',99,7,'openai','gpt-5',1,10,20,1.5,2,2.5)`)
	if err := VerifyPending(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	readyCount(2)
	exec(`UPDATE usage_api_key_hourly_rollups SET input_tokens=11`)
	exec(`UPDATE usage_rollup_coverage SET state='pending' WHERE projection='usage_api_key_hourly_rollups'`)
	if err := VerifyPending(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	readyCount(1)
}
