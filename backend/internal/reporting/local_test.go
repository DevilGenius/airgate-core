package reporting_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/config"
	_ "github.com/lib/pq"
)

// Opt-in, read-only verification against the explicitly selected local dev
// server and its database. Credentials and response records are never logged.
func TestLocalReportingR01(t *testing.T) {
	configPath := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if configPath == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG to the local dev config")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal("load local test configuration failed")
	}
	token, err := auth.NewJWTManager(cfg.JWT.Secret, 1).GenerateToken(1, "admin", "")
	if err != nil {
		t.Fatal("create local test identity failed")
	}
	client := &http.Client{Timeout: 35 * time.Second}
	get := func(path, query string, expectedStatus int, result any) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:9517"+path+"?"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		started := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("local request failed: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			t.Fatal("read local response failed")
		}
		if resp.StatusCode != expectedStatus {
			t.Fatalf("%s: HTTP %d, want %d", path, resp.StatusCode, expectedStatus)
		}
		if result != nil {
			var envelope struct {
				Code int
				Data json.RawMessage
			}
			if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code != 0 {
				t.Fatal("invalid local response")
			}
			if err := json.Unmarshal(envelope.Data, result); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s HTTP %d (%s)", path, resp.StatusCode, time.Since(started).Round(time.Millisecond))
	}
	for _, query := range []string{
		"start_date=invalid", "end_date=2026-02-30", "tz=invalid%2Fzone", "granularity=minute",
		"start_date=2020-01-01&end_date=2026-09-07&granularity=hour",
	} {
		get("/api/v1/admin/usage/trend", query, 400, nil)
	}

	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var latest time.Time
	if err := db.QueryRowContext(ctx, `SELECT created_at FROM usage_logs WHERE created_at < $1 ORDER BY created_at DESC LIMIT 1`, time.Now().UTC().Truncate(24*time.Hour)).Scan(&latest); err != nil {
		if err == sql.ErrNoRows {
			t.Skip("local database has no closed-day usage to compare")
		}
		t.Fatal("select closed-day fixture failed")
	}
	day := latest.UTC().Truncate(24 * time.Hour)
	end := day.AddDate(0, 0, 1)
	params := url.Values{"start_date": {day.Format("2006-01-02")}, "end_date": {day.Format("2006-01-02")}, "tz": {"UTC"}, "granularity": {"hour"}}
	var buckets []struct {
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		StandardCost float64 `json:"standard_cost"`
	}
	get("/api/v1/admin/usage/trend", params.Encode(), 200, &buckets)
	var wantInput, wantOutput int64
	var wantCost float64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),COALESCE(SUM(total_cost),0) FROM usage_logs WHERE created_at >= $1 AND created_at < $2`, day, end).Scan(&wantInput, &wantOutput, &wantCost); err != nil {
		t.Fatal("local trend reference query failed")
	}
	var gotInput, gotOutput int64
	var gotCost float64
	for _, bucket := range buckets {
		gotInput += bucket.InputTokens
		gotOutput += bucket.OutputTokens
		gotCost += bucket.StandardCost
	}
	if len(buckets) > 24 || gotInput != wantInput || gotOutput != wantOutput || math.Abs(gotCost-wantCost) > 1e-6 {
		t.Fatal("trend aggregate differs from detail reference")
	}
	t.Logf("trend reference matched; buckets=%d", len(buckets))

	var accountID int
	err = db.QueryRowContext(ctx, `SELECT account_usage_logs FROM usage_logs l JOIN accounts a ON a.id=l.account_usage_logs AND a.deleted_at IS NULL WHERE l.created_at >= $1 AND l.created_at < $2 LIMIT 1`, day, end).Scan(&accountID)
	if err == sql.ErrNoRows {
		t.Log("no account usage in the closed day; account comparison skipped")
		return
	}
	if err != nil {
		t.Fatal("select local test account failed")
	}
	var result struct {
		Range struct {
			Count       int
			InputTokens int64   `json:"input_tokens"`
			AccountCost float64 `json:"account_cost"`
		}
	}
	get(fmt.Sprintf("/api/v1/admin/accounts/%d/stats", accountID), params.Encode(), 200, &result)
	var wantCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(input_tokens),0),COALESCE(SUM(account_cost),0) FROM usage_logs WHERE account_usage_logs=$1 AND created_at >= $2 AND created_at < $3`, accountID, day, end).Scan(&wantCount, &wantInput, &wantCost); err != nil {
		t.Fatal("local account reference query failed")
	}
	if result.Range.Count != wantCount || result.Range.InputTokens != wantInput || math.Abs(result.Range.AccountCost-wantCost) > 1e-6 {
		t.Fatal("account aggregate differs from detail reference")
	}
	t.Log("account count, tokens and cost matched detail reference")
}

func TestLocalUnverifiedHistoryR07(t *testing.T) {
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG to the local dev config")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local test configuration failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var state string
	if err := db.QueryRowContext(ctx, `SELECT state FROM usage_rollup_coverage WHERE projection='usage_api_key_hourly_rollups'`).Scan(&state); err != nil {
		t.Fatal("read local projection coverage failed")
	}
	if state == "ready" {
		t.Skip("historical backfill already verified; pending-state guard is covered by isolated tests")
	}
	token, err := auth.NewJWTManager(cfg.JWT.Secret, 1).GenerateToken(1, "admin", "")
	if err != nil {
		t.Fatal("create local test identity failed")
	}
	endpoint := "http://127.0.0.1:9517/api/v1/admin/usage/stats?model=r07-coverage-" + fmt.Sprint(time.Now().UnixNano())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal("local coverage request failed")
	}
	defer resp.Body.Close()
	var result struct {
		Code    int
		Message string
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		t.Fatal("decode local coverage response failed")
	}
	if resp.StatusCode != 503 || result.Code != 503 || result.Message != "历史统计尚未完成同步，请稍后重试" {
		t.Fatalf("unverified coverage returned HTTP %d / code %d", resp.StatusCode, result.Code)
	}
	t.Log("pending historical coverage returned explicit HTTP 503 instead of a zero total")
}

func TestLocalReportingReadyR07(t *testing.T) {
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG to the local dev config")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local configuration failed")
	}
	token, err := auth.NewJWTManager(cfg.JWT.Secret, 1).GenerateToken(1, "admin", "")
	if err != nil {
		t.Fatal("create local identity failed")
	}
	for _, path := range []string{"/api/v1/admin/dashboard/stats", "/api/v1/admin/dashboard/trend?range=today&granularity=hour", "/api/v1/admin/usage/stats"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:9517"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			t.Fatal("local request failed")
		}
		var result struct {
			Code int
			Data json.RawMessage
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || result.Code != 0 || len(result.Data) == 0 {
			t.Fatalf("%s: HTTP %d, code %d", path, resp.StatusCode, result.Code)
		}
		t.Logf("%s HTTP 200", path)
	}
}
