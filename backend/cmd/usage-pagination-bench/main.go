package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/config"
)

type row struct {
	ID       int64 `json:"id"`
	APIKeyID int64 `json:"api_key_id"`
	UserID   int64 `json:"user_id"`
}
type page struct {
	List       []row `json:"list"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	TotalExact bool  `json:"total_exact"`
}
type metadata struct {
	Status     string    `json:"status"`
	Snapshot   string    `json:"snapshot"`
	Total      int64     `json:"total"`
	CreatedAt  time.Time `json:"created_at"`
	Refreshing bool      `json:"refreshing"`
}

var output = json.NewEncoder(os.Stdout)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	configPath := flag.String("config", "../.dev/config.yaml", "Local test configuration")
	base := flag.String("base", "http://127.0.0.1:9517", "Local backend")
	role := flag.String("role", "admin", "admin, user or api_key")
	actor := flag.Int("actor", 1, "Existing local test user")
	key := flag.Int("key", 0, "API key ID filter")
	model := flag.String("model", "", "Model substring filter")
	verify := flag.Bool("verify-db", false, "Compare exact count and a deep page with read-only SQL")
	refresh := flag.Bool("refresh", false, "Wait for a freshly prepared browsing snapshot")
	concurrency := flag.Int("concurrency", 0, "Optional concurrent page readers (1-32)")
	requests := flag.Int("requests", 80, "Concurrent page request count")
	flag.Parse()
	if (*role != "admin" && *role != "user" && *role != "api_key") || *actor <= 0 || *key < 0 || *concurrency < 0 || *concurrency > 32 || *requests < 1 || *requests > 1000 {
		panic("invalid benchmark arguments")
	}
	baseURL, err := url.Parse(*base)
	must(err)
	if baseURL.Scheme != "http" || (baseURL.Hostname() != "localhost" && baseURL.Hostname() != "127.0.0.1" && baseURL.Hostname() != "::1") {
		panic("benchmark target must be the local HTTP test server")
	}
	cfg, err := config.Load(*configPath)
	must(err)
	db, err := sql.Open("postgres", cfg.Database.DSN())
	must(err)
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if *role == "api_key" && *key == 0 {
		must(db.QueryRowContext(ctx, "SELECT id FROM api_keys WHERE user_api_keys=$1 AND status='active' AND (expires_at IS NULL OR expires_at>now()) ORDER BY used_quota DESC LIMIT 1", *actor).Scan(key))
	}
	issuer := auth.NewJWTManager(cfg.JWT.Secret, 1)
	var token string
	if *role == "api_key" {
		token, err = issuer.GenerateAPIKeyToken(*actor, auth.APIKeySessionRole, "", *key)
	} else {
		token, err = issuer.GenerateToken(*actor, *role, "")
	}
	must(err)
	client := &http.Client{Timeout: 30 * time.Second}
	endpoint := "/api/v1/usage"
	if *role == "admin" {
		endpoint = "/api/v1/admin/usage"
	}
	params := url.Values{"page": {"1"}, "page_size": {"20"}, "tz": {"UTC"}}
	if *key > 0 {
		params.Set("api_key_id", strconv.Itoa(*key))
	}
	if *model != "" {
		params.Set("model", *model)
	}
	get := func(path string, params url.Values, result any) (time.Duration, []byte, int) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, *base+path+"?"+params.Encode(), nil)
		must(err)
		req.Header.Set("Authorization", "Bearer "+token)
		start := time.Now()
		response, err := client.Do(req)
		must(err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		must(err)
		if response.StatusCode != http.StatusOK {
			return time.Since(start), body, response.StatusCode
		}
		var envelope struct {
			Code    int             `json:"code"`
			Data    json.RawMessage `json:"data"`
			Message string          `json:"message"`
		}
		must(json.Unmarshal(body, &envelope))
		if envelope.Code != 0 {
			panic(envelope.Message)
		}
		must(json.Unmarshal(envelope.Data, result))
		return time.Since(start), body, response.StatusCode
	}
	check := func(status int, body []byte) {
		if status != 200 {
			panic(fmt.Sprintf("HTTP %d: %s", status, body))
		}
	}
	var live page
	duration, body, status := get(endpoint, params, &live)
	check(status, body)
	must(output.Encode(map[string]any{"case": "latest", "role": *role, "key": *key, "model": *model, "duration_ms": float64(duration.Microseconds()) / 1000, "rows": len(live.List)}))
	started := time.Now()
	var meta metadata
	if *refresh {
		params.Set("refresh_pagination", "true")
	}
	for {
		_, body, status = get(endpoint+"/pagination", params, &meta)
		check(status, body)
		if meta.Status == "ready" && (!*refresh || !meta.CreatedAt.Before(started.Add(-time.Second))) {
			break
		}
		if meta.Status == "failed" {
			panic("pagination index failed")
		}
		if meta.Status == "preparing" || meta.Refreshing {
			params.Del("refresh_pagination")
		} else if *refresh {
			params.Set("refresh_pagination", "true")
		}
		if time.Since(started) > 2*time.Minute {
			panic("pagination preparation timed out")
		}
		time.Sleep(500 * time.Millisecond)
	}
	must(output.Encode(map[string]any{"case": "metadata", "role": *role, "key": *key, "model": *model, "prepare_ms": float64(time.Since(started).Microseconds()) / 1000, "total": meta.Total, "snapshot_at": meta.CreatedAt}))
	last := max(1, int((meta.Total+19)/20))
	middle := max(1, last/2)
	params.Del("refresh_pagination")
	pages := []int{1, min(1000, last), middle, last}
	var middleIDs []int64
	for repeat := 0; repeat < 3; repeat++ {
		for _, target := range pages {
			params.Set("page", strconv.Itoa(target))
			params.Set("snapshot", meta.Snapshot)
			var data page
			duration, body, status = get(endpoint, params, &data)
			check(status, body)
			if data.Page != target || data.Total != meta.Total || !data.TotalExact {
				panic("page metadata mismatch")
			}
			ids := make([]int64, 0, len(data.List))
			for i, item := range data.List {
				if i > 0 && data.List[i-1].ID <= item.ID {
					panic("invalid ID order")
				}
				if *key > 0 && item.APIKeyID != int64(*key) {
					panic("API Key filter escaped")
				}
				ids = append(ids, item.ID)
			}
			if *role == "api_key" && strings.Contains(string(body), "\"actual_cost\"") {
				panic("internal costs leaked to API key session")
			}
			if target == middle {
				if repeat == 0 {
					middleIDs = ids
				} else if !reflect.DeepEqual(middleIDs, ids) {
					panic("pinned page shifted")
				}
			}
			must(output.Encode(map[string]any{"case": "page", "role": *role, "key": *key, "model": *model, "page": target, "repeat": repeat, "duration_ms": float64(duration.Microseconds()) / 1000, "rows": len(ids)}))
		}
	}
	if *verify {
		where := []string{"1=1"}
		args := []any{}
		add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
		if *role != "admin" {
			where = append(where, "COALESCE(NULLIF(user_id_snapshot,0),user_usage_logs,0)="+add(*actor))
		}
		if *key > 0 {
			where = append(where, "api_key_usage_logs="+add(*key))
		}
		if *model != "" {
			where = append(where, "model LIKE "+add("%"+*model+"%"))
		}
		predicates := strings.Join(where, " AND ")
		var count int64
		started := time.Now()
		must(db.QueryRowContext(ctx, "SELECT count(*) FROM usage_logs WHERE "+predicates, args...).Scan(&count))
		if count != meta.Total {
			panic(fmt.Sprintf("count=%d index=%d", count, meta.Total))
		}
		must(output.Encode(map[string]any{"case": "verified_count", "role": *role, "total": count, "duration_ms": float64(time.Since(started).Microseconds()) / 1000}))
		started = time.Now()
		rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT id FROM usage_logs WHERE %s ORDER BY id DESC OFFSET %d LIMIT 20", predicates, (middle-1)*20), args...)
		must(err)
		want := []int64{}
		for rows.Next() {
			var id int64
			must(rows.Scan(&id))
			want = append(want, id)
		}
		must(rows.Err())
		must(rows.Close())
		if !reflect.DeepEqual(want, middleIDs) {
			panic(fmt.Sprintf("deep page mismatch got=%v expected=%v", middleIDs, want))
		}
		must(output.Encode(map[string]any{"case": "verified_deep_offset", "role": *role, "page": middle, "duration_ms": float64(time.Since(started).Microseconds()) / 1000}))
	}
	if *concurrency > 0 {
		samples := make([]float64, *requests)
		jobs := make(chan int)
		var workers sync.WaitGroup
		for range *concurrency {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for i := range jobs {
					query := url.Values{}
					for name, values := range params {
						query[name] = append([]string(nil), values...)
					}
					query.Set("page", strconv.Itoa(1+(i*7919)%last))
					var data page
					elapsed, body, status := get(endpoint, query, &data)
					check(status, body)
					if !data.TotalExact || data.Total != meta.Total {
						panic("concurrent page metadata mismatch")
					}
					samples[i] = float64(elapsed.Microseconds()) / 1000
				}
			}()
		}
		started := time.Now()
		for i := range *requests {
			jobs <- i
		}
		close(jobs)
		workers.Wait()
		wall := time.Since(started)
		sort.Float64s(samples)
		must(output.Encode(map[string]any{"case": "concurrent_pages", "role": *role, "readers": *concurrency, "requests": *requests, "p50_ms": samples[len(samples)/2], "p95_ms": samples[(len(samples)-1)*95/100], "max_ms": samples[len(samples)-1], "requests_per_second": float64(*requests) / wall.Seconds()}))
	}
	params.Set("model", "different-filter")
	params.Set("page", "2")
	_, body, status = get(endpoint, params, &page{})
	if status != 409 {
		panic(fmt.Sprintf("changed filter accepted snapshot: HTTP %d %s", status, body))
	}
	must(output.Encode(map[string]any{"case": "filter_binding_verified", "role": *role}))
}
