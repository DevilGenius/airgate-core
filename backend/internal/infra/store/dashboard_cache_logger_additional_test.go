package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"

	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
)

func TestEntLoggerRedactionAndEmailHash(t *testing.T) {
	if EntSlogLogger() == nil {
		t.Fatal("EntSlogLogger returned nil")
	}

	if got := RedactDSN(""); got != "" {
		t.Fatalf("empty RedactDSN = %q", got)
	}
	if got := RedactDSN("host=db port=5432 user=app password=secret dbname=airgate"); got != "host=db port=5432 user=app password=*** dbname=airgate" {
		t.Fatalf("keyword RedactDSN = %q", got)
	}
	if got := RedactDSN("HOST=db PASSWORD=secret"); got != "HOST=db password=***" {
		t.Fatalf("case-insensitive RedactDSN = %q", got)
	}
	if got := RedactDSN("postgres://user:secret@example.com:5432/airgate?sslmode=disable"); !strings.Contains(got, "user:%2A%2A%2A@example.com") || strings.Contains(got, "secret") {
		t.Fatalf("url RedactDSN = %q", got)
	}
	if got := RedactDSN("postgres://user@example.com/db"); got != "postgres://user@example.com/db" {
		t.Fatalf("url without password = %q", got)
	}
	if got := RedactDSN("://bad password=secret"); got != "://bad password=***" {
		t.Fatalf("invalid-url fallback RedactDSN = %q", got)
	}

	for _, tt := range []struct {
		email string
		want  string
	}{
		{"joineroz749@gmail.com", "joi***@gmail.com"},
		{"a@b.com", "a***@b.com"},
		{"@b.com", "***"},
		{"not-email", "***"},
	} {
		if got := EmailHash(tt.email); got != tt.want {
			t.Fatalf("EmailHash(%q) = %q, want %q", tt.email, got, tt.want)
		}
	}
}

func TestDashboardStatsCacheHelpers(t *testing.T) {
	ctx := context.Background()
	today := time.Date(2026, 6, 20, 0, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	key := dashboardStatsCacheKey(7, today)
	lockKey := dashboardStatsLockKey(7, today)
	if !strings.Contains(key, "ag:dashboard:stats:7:") || !strings.Contains(lockKey, "ag:dashboard:stats:lock:7:") {
		t.Fatalf("dashboard keys = %q %q", key, lockKey)
	}

	storeWithoutRedis := NewDashboardStore(nil)
	if storeWithoutRedis.rdb != nil {
		t.Fatal("NewDashboardStore without redis should leave rdb nil")
	}
	if snapshot, ok := storeWithoutRedis.loadStatsSnapshotCache(ctx, 7, today); ok || snapshot.TotalAPIKeys != 0 {
		t.Fatalf("nil redis load cache = %+v %v", snapshot, ok)
	}
	storeWithoutRedis.storeStatsSnapshotCache(ctx, 7, today, appdashboard.StatsSnapshot{TotalAPIKeys: 1})
	if token, ok, busy := storeWithoutRedis.tryLockStatsSnapshot(ctx, 7, today); token != "" || ok || busy {
		t.Fatalf("nil redis lock = %q %v %v", token, ok, busy)
	}
	storeWithoutRedis.releaseStatsSnapshotLock(ctx, 7, today, "token")
	if snapshot, ok := storeWithoutRedis.waitForStatsSnapshotCache(ctx, 7, today, time.Millisecond); ok || snapshot.TotalAPIKeys != 0 {
		t.Fatalf("nil redis wait cache = %+v %v", snapshot, ok)
	}

	snapshot := appdashboard.StatsSnapshot{TotalAPIKeys: 11, TodayRequests: 22}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	rdb, mock := redismock.NewClientMock()
	storeWithRedis := NewDashboardStore(nil, rdb)
	mock.ExpectGet(key).SetVal(string(raw))
	got, ok := storeWithRedis.loadStatsSnapshotCache(ctx, 7, today)
	if !ok || got.TotalAPIKeys != 11 || got.TodayRequests != 22 {
		t.Fatalf("load cache = %+v %v", got, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("cache load expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.ExpectGet(key).SetVal("{")
	mock.ExpectDel(key).SetVal(1)
	if got, ok := storeWithRedis.loadStatsSnapshotCache(ctx, 7, today); ok || got.TotalAPIKeys != 0 {
		t.Fatalf("bad JSON cache = %+v %v", got, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("bad cache expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.ExpectGet(key).SetErr(redis.Nil)
	if got, ok := storeWithRedis.loadStatsSnapshotCache(ctx, 7, today); ok || got.TotalAPIKeys != 0 {
		t.Fatalf("missing cache = %+v %v", got, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("missing cache expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.Regexp().ExpectSet(key, ".*", dashboardStatsCacheTTL).SetVal("OK")
	storeWithRedis.storeStatsSnapshotCache(ctx, 7, today, snapshot)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("store cache expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.Regexp().ExpectSetNX(lockKey, ".+", dashboardStatsLockTTL).SetVal(true)
	token, ok, busy := storeWithRedis.tryLockStatsSnapshot(ctx, 7, today)
	if !ok || busy || token == "" {
		t.Fatalf("lock acquired = %q %v %v", token, ok, busy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("lock acquired expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.Regexp().ExpectSetNX(lockKey, ".+", dashboardStatsLockTTL).SetVal(false)
	token, ok, busy = storeWithRedis.tryLockStatsSnapshot(ctx, 7, today)
	if token != "" || ok || !busy {
		t.Fatalf("lock busy = %q %v %v", token, ok, busy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("lock busy expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.Regexp().ExpectSetNX(lockKey, ".+", dashboardStatsLockTTL).SetErr(redis.Nil)
	token, ok, busy = storeWithRedis.tryLockStatsSnapshot(ctx, 7, today)
	if token != "" || ok || busy {
		t.Fatalf("lock error = %q %v %v", token, ok, busy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("lock error expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.ExpectEvalSha(dashboardStatsLockReleaseScript.Hash(), []string{lockKey}, "token").SetVal(int64(1))
	storeWithRedis.releaseStatsSnapshotLock(ctx, 7, today, "token")
	storeWithRedis.releaseStatsSnapshotLock(ctx, 7, today, "")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("release expectations: %v", err)
	}
}

func TestDashboardStatsWaitAndLoadCachePaths(t *testing.T) {
	ctx := context.Background()
	today := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	fiveMinAgo := today.Add(12 * time.Hour)
	key := dashboardStatsCacheKey(0, today)
	lockKey := dashboardStatsLockKey(0, today)
	snapshot := appdashboard.StatsSnapshot{TotalAPIKeys: 3, TodayRequests: 4}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	rdb, mock := redismock.NewClientMock()
	storeWithRedis := NewDashboardStore(nil, rdb)
	mock.ExpectGet(key).SetVal(string(raw))
	got, ok := storeWithRedis.waitForStatsSnapshotCache(ctx, 0, today, time.Second)
	if !ok || got.TotalAPIKeys != 3 {
		t.Fatalf("wait cache hit = %+v %v", got, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("wait hit expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.ExpectGet(key).SetErr(redis.Nil)
	got, ok = storeWithRedis.waitForStatsSnapshotCache(ctx, 0, today, 0)
	if ok || got.TotalAPIKeys != 0 {
		t.Fatalf("wait timeout = %+v %v", got, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("wait timeout expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	mock.ExpectGet(key).SetErr(redis.Nil)
	got, ok = storeWithRedis.waitForStatsSnapshotCache(canceled, 0, today, time.Second)
	if ok || got.TotalAPIKeys != 0 {
		t.Fatalf("wait canceled = %+v %v", got, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("wait canceled expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.ExpectGet(key).SetVal(string(raw))
	got, err = storeWithRedis.LoadStatsSnapshot(ctx, today, fiveMinAgo, 0)
	if err != nil || got.TodayRequests != 4 {
		t.Fatalf("LoadStatsSnapshot cache = %+v %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("load cache expectations: %v", err)
	}

	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(db, rdb)
	mock.ExpectGet(key).SetErr(redis.Nil)
	mock.Regexp().ExpectSetNX(lockKey, ".+", dashboardStatsLockTTL).SetVal(true)
	mock.ExpectGet(key).SetErr(redis.Nil)
	mock.Regexp().ExpectSet(key, ".*", dashboardStatsCacheTTL).SetVal("OK")
	mock.Regexp().ExpectEvalSha(dashboardStatsLockReleaseScript.Hash(), []string{lockKey}, ".+").SetVal(int64(1))
	got, err = storeWithRedis.LoadStatsSnapshot(ctx, today, fiveMinAgo, 0)
	if err != nil || got.TotalAPIKeys != 0 {
		t.Fatalf("LoadStatsSnapshot lock fresh = %+v %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("lock fresh expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	storeWithRedis = NewDashboardStore(nil, rdb)
	mock.ExpectGet(key).SetErr(redis.Nil)
	mock.Regexp().ExpectSetNX(lockKey, ".+", dashboardStatsLockTTL).SetVal(false)
	mock.ExpectGet(key).SetVal(string(raw))
	got, err = storeWithRedis.LoadStatsSnapshot(ctx, today, fiveMinAgo, 0)
	if err != nil || got.TotalAPIKeys != 3 {
		t.Fatalf("LoadStatsSnapshot lock busy cache = %+v %v", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("lock busy expectations: %v", err)
	}
}
