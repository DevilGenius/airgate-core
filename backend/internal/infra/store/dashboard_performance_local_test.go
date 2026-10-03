package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/DevilGenius/airgate-core/ent"
	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/config"
)

// Opt-in read-only measurements against the local development database/server.
// Report timings and counts only; never log credentials or individual records.
func TestLocalDashboardPerformance(t *testing.T) {
	path := os.Getenv("AIRGATE_DASHBOARD_PERF_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_DASHBOARD_PERF_CONFIG to a local dev config")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local config failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN()+" default_transaction_read_only=on statement_timeout=20000 application_name=airgate-dashboard-perf")
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(3)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	store := NewDashboardStore(client)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	end := time.Now().In(loc)
	day := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)
	start := day.AddDate(0, 0, -89)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	measure := func(name string, call func() (int, error)) {
		t.Helper()
		started := time.Now()
		count, err := call()
		if err != nil {
			t.Fatalf("%s failed: %v", name, err)
		}
		t.Logf("%s: %s, rows=%d", name, time.Since(started).Round(time.Millisecond), count)
	}
	measure("hourly model/user", func() (int, error) {
		rows, err := store.ListTrendLogs(ctx, start, end, 0)
		return len(rows), err
	})
	measure("hourly API keys", func() (int, error) {
		rows, err := store.ListAPIKeyTrendLogs(ctx, start, end, 0, "day", loc)
		return len(rows), err
	})
	measure("account/group distribution", func() (int, error) {
		rows, err := store.LoadDistributionStats(ctx, start, end, 0)
		return len(rows.Accounts) + len(rows.Groups), err
	})
	measure("uncached trend service", func() (int, error) {
		result, err := appdashboard.NewService(store).Trend(ctx, appdashboard.TrendQuery{Range: "90d", Granularity: "day", TZ: "Asia/Shanghai"})
		return len(result.AccountDistribution), err
	})
	if os.Getenv("AIRGATE_DASHBOARD_PERF_HTTP") == "1" {
		token, err := auth.NewJWTManager(cfg.JWT.Secret, 1).GenerateToken(1, "admin", "")
		if err != nil {
			t.Fatal("create local test identity failed")
		}
		for i := 0; i < 2; i++ {
			started := time.Now()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:9517/api/v1/admin/dashboard/trend?range=90d&granularity=day&tz=Asia%2FShanghai", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal("local HTTP request failed")
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			_ = resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK || !json.Valid(body) {
				t.Fatalf("local HTTP response invalid: status=%d", resp.StatusCode)
			}
			t.Logf("HTTP trend request %d: %s, bytes=%d", i+1, time.Since(started).Round(time.Millisecond), len(body))
		}
	}
}

func TestLocalDashboardAPIKeyEquivalence(t *testing.T) {
	path := os.Getenv("AIRGATE_DASHBOARD_PERF_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_DASHBOARD_PERF_CONFIG to a local dev config")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local config failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN()+" default_transaction_read_only=on statement_timeout=20000")
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer func() { _ = db.Close() }()
	store := NewDashboardStore(ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db))))
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -90).Truncate(24 * time.Hour)
	var selectedUser int
	if err := db.QueryRowContext(t.Context(), `SELECT user_id FROM usage_api_key_hourly_rollups WHERE user_id > 0 GROUP BY user_id ORDER BY SUM(requests) DESC LIMIT 1`).Scan(&selectedUser); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []int{0, selectedUser} {
		// Baseline is the former endpoint's all-key/hour aggregation, before
		// Top12 selection and caller-local time bucketing in Go.
		rows, err := db.QueryContext(t.Context(), `SELECT api_key_id, bucket_start,
SUM(requests)::bigint, SUM(input_tokens+output_tokens+cached_input_tokens+cache_creation_tokens)::bigint,
SUM(billed_cost)::double precision FROM usage_api_key_hourly_rollups
WHERE bucket_start >= $1 AND bucket_start < $2 AND ($3::integer=0 OR user_id=$3)
GROUP BY api_key_id,bucket_start`, start, end, userID)
		if err != nil {
			t.Fatal(err)
		}
		var baseline []appdashboard.APIKeyTrendLog
		totals := map[int]int64{}
		for rows.Next() {
			var item appdashboard.APIKeyTrendLog
			if err := rows.Scan(&item.APIKeyID, &item.CreatedAt, &item.Requests, &item.Tokens, &item.BilledCost); err != nil {
				t.Fatal(err)
			}
			baseline = append(baseline, item)
			totals[item.APIKeyID] += item.Tokens
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		ids := make([]int, 0, len(totals))
		for id := range totals {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if totals[ids[i]] == totals[ids[j]] {
				return ids[i] < ids[j]
			}
			return totals[ids[i]] > totals[ids[j]]
		})
		if len(ids) > 12 {
			ids = ids[:12]
		}
		selected := map[int]bool{}
		for _, id := range ids {
			selected[id] = true
		}
		for _, zone := range []string{"Asia/Shanghai", "America/New_York", "Asia/Kathmandu"} {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			for _, granularity := range []string{"day", "hour"} {
				layout := "2006-01-02"
				if granularity == "hour" {
					layout = "2006-01-02 15:00"
				}
				type key struct {
					id     int
					bucket string
				}
				type sum struct {
					requests, tokens int64
					cost             float64
				}
				aggregate := func(items []appdashboard.APIKeyTrendLog) map[key]sum {
					result := map[key]sum{}
					for _, item := range items {
						if !selected[item.APIKeyID] {
							continue
						}
						k := key{item.APIKeyID, item.CreatedAt.In(loc).Format(layout)}
						v := result[k]
						v.requests += item.Requests
						v.tokens += item.Tokens
						v.cost += item.BilledCost
						result[k] = v
					}
					return result
				}
				optimized, err := store.ListAPIKeyTrendLogs(t.Context(), start, end, userID, granularity, loc)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range optimized {
					if !selected[item.APIKeyID] {
						t.Fatal("query returned a key outside the global Top12")
					}
				}
				want, got := aggregate(baseline), aggregate(optimized)
				if len(want) != len(got) {
					t.Fatalf("%s/%s bucket count differs: %d vs %d", zone, granularity, len(got), len(want))
				}
				for key, w := range want {
					g, ok := got[key]
					if !ok || g.requests != w.requests || g.tokens != w.tokens || math.Abs(g.cost-w.cost) > 1e-6 {
						t.Fatalf("%s/%s totals differ", zone, granularity)
					}
				}
				t.Logf("scope filtered=%v %s/%s: matched %d buckets", userID > 0, zone, granularity, len(got))
			}
		}
	}
}
