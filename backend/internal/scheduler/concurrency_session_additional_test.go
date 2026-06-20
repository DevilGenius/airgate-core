package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/ent"
)

type capacityEvent struct {
	accountID int
	current   int
}

type recordingCapacityPublisher struct {
	events []capacityEvent
}

func (p *recordingCapacityPublisher) PublishAccountCapacityChanged(accountID int, currentConcurrency int) {
	p.events = append(p.events, capacityEvent{accountID: accountID, current: currentConcurrency})
}

func TestConcurrencyManagerNilKeysPublishAndParse(t *testing.T) {
	ctx := context.Background()
	var nilManager *ConcurrencyManager
	nilManager.SetCapacityEventPublisher(&recordingCapacityPublisher{})
	nilManager.publishAccountCapacity(7, 1)

	cm := NewConcurrencyManager(nil)
	if cm == nil || cm.rdb != nil {
		t.Fatalf("NewConcurrencyManager(nil) = %#v", cm)
	}
	if got := concurrencyKey(7); got != "ag:concurrency:account:7" {
		t.Fatalf("concurrencyKey = %q", got)
	}
	if got := concurrencyCountKey(7); got != "ag:concurrency:account:7:count" {
		t.Fatalf("concurrencyCountKey = %q", got)
	}
	if got := apiKeyConcurrencyKey(8); got != "ag:concurrency:apikey:8" {
		t.Fatalf("apiKeyConcurrencyKey = %q", got)
	}
	if got := apiKeyConcurrencyCountKey(8); got != "ag:concurrency:apikey:8:count" {
		t.Fatalf("apiKeyConcurrencyCountKey = %q", got)
	}
	if got := userConcurrencyKey(9); got != "ag:concurrency:user:9" {
		t.Fatalf("userConcurrencyKey = %q", got)
	}
	if got := userConcurrencyCountKey(9); got != "ag:concurrency:user:9:count" {
		t.Fatalf("userConcurrencyCountKey = %q", got)
	}

	if err := cm.AcquireSlot(ctx, 7, "req", 0, 0); err != nil {
		t.Fatalf("AcquireSlot nil redis = %v", err)
	}
	cm.ReleaseSlot(ctx, 7, "req")
	if err := cm.AcquireAPIKeySlot(ctx, 8, "req", 0, 0); err != nil {
		t.Fatalf("AcquireAPIKeySlot nil redis = %v", err)
	}
	cm.ReleaseAPIKeySlot(ctx, 8, "req")
	if err := cm.AcquireUserSlot(ctx, 9, "req", 0, 0); err != nil {
		t.Fatalf("AcquireUserSlot nil redis = %v", err)
	}
	cm.ReleaseUserSlot(ctx, 9, "req")
	if got := cm.GetCurrentCount(ctx, 7); got != 0 {
		t.Fatalf("GetCurrentCount nil redis = %d", got)
	}
	if got := cm.GetCurrentCounts(ctx, []int{7}); len(got) != 0 {
		t.Fatalf("GetCurrentCounts nil redis = %#v", got)
	}

	publisher := &recordingCapacityPublisher{}
	cm.SetCapacityEventPublisher(publisher)
	cm.publishAccountCapacity(7, -5)
	cm.publishAccountCapacity(0, 3)
	if len(publisher.events) != 1 || publisher.events[0] != (capacityEvent{accountID: 7, current: 0}) {
		t.Fatalf("capacity events = %#v", publisher.events)
	}

	changed, current, ok := parseSlotScriptResult([]interface{}{int64(1), "2"})
	if !ok || changed != 1 || current != 2 {
		t.Fatalf("parseSlotScriptResult valid = %d, %d, %v", changed, current, ok)
	}
	for _, raw := range []any{
		nil,
		[]interface{}{int64(1)},
		[]interface{}{"bad", int64(2)},
		[]interface{}{int64(1), "bad"},
	} {
		if _, _, ok := parseSlotScriptResult(raw); ok {
			t.Fatalf("parseSlotScriptResult(%#v) ok = true", raw)
		}
	}
}

func TestConcurrencyManagerAcquireReleaseScripts(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	cm := NewConcurrencyManager(rdb)
	publisher := &recordingCapacityPublisher{}
	cm.SetCapacityEventPublisher(publisher)

	mock.Regexp().ExpectEvalSha(acquireSlotScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		`^\d+$`, "2", "req-1", "1",
	).SetVal([]interface{}{int64(1), int64(2)})
	if err := cm.AcquireSlot(ctx, 7, "req-1", 2, time.Second); err != nil {
		t.Fatalf("AcquireSlot allowed = %v", err)
	}
	if len(publisher.events) != 1 || publisher.events[0] != (capacityEvent{accountID: 7, current: 2}) {
		t.Fatalf("capacity events after acquire = %#v", publisher.events)
	}

	mock.Regexp().ExpectEvalSha(acquireSlotScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		`^\d+$`, "2", "req-2", "1",
	).SetVal([]interface{}{int64(0), int64(2)})
	if err := cm.AcquireSlot(ctx, 7, "req-2", 2, time.Second); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("AcquireSlot full = %v, want ErrConcurrencyLimit", err)
	}

	mock.ExpectEvalSha(releaseSlotScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		"req-1", int(defaultSlotTTL.Seconds()), int(concurrencyZeroCountTTL.Seconds()),
	).SetVal([]interface{}{int64(1), int64(1)})
	cm.ReleaseSlot(ctx, 7, "req-1")
	if len(publisher.events) != 2 || publisher.events[1] != (capacityEvent{accountID: 7, current: 1}) {
		t.Fatalf("capacity events after release = %#v", publisher.events)
	}

	mock.ExpectEvalSha(releaseSlotScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		"missing", int(defaultSlotTTL.Seconds()), int(concurrencyZeroCountTTL.Seconds()),
	).SetVal([]interface{}{int64(0), int64(1)})
	cm.ReleaseSlot(ctx, 7, "missing")
	if len(publisher.events) != 2 {
		t.Fatalf("unexpected capacity event after no-op release: %#v", publisher.events)
	}

	mock.Regexp().ExpectEvalSha(acquireSlotScript.Hash(), []string{apiKeyConcurrencyKey(8), apiKeyConcurrencyCountKey(8)},
		`^\d+$`, "1", "req-api", "300",
	).SetVal([]interface{}{int64(1), int64(1)})
	if err := cm.AcquireAPIKeySlot(ctx, 8, "req-api", 1, 0); err != nil {
		t.Fatalf("AcquireAPIKeySlot = %v", err)
	}
	mock.ExpectEvalSha(releaseSlotScript.Hash(), []string{apiKeyConcurrencyKey(8), apiKeyConcurrencyCountKey(8)},
		"req-api", int(defaultSlotTTL.Seconds()), int(concurrencyZeroCountTTL.Seconds()),
	).SetVal([]interface{}{int64(1), int64(0)})
	cm.ReleaseAPIKeySlot(ctx, 8, "req-api")

	mock.Regexp().ExpectEvalSha(acquireSlotScript.Hash(), []string{userConcurrencyKey(9), userConcurrencyCountKey(9)},
		`^\d+$`, "1", "req-user", "300",
	).SetVal([]interface{}{int64(1), int64(1)})
	if err := cm.AcquireUserSlot(ctx, 9, "req-user", 1, 0); err != nil {
		t.Fatalf("AcquireUserSlot = %v", err)
	}
	mock.ExpectEvalSha(releaseSlotScript.Hash(), []string{userConcurrencyKey(9), userConcurrencyCountKey(9)},
		"req-user", int(defaultSlotTTL.Seconds()), int(concurrencyZeroCountTTL.Seconds()),
	).SetVal([]interface{}{int64(1), int64(0)})
	cm.ReleaseUserSlot(ctx, 9, "req-user")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestConcurrencyManagerFailOpenAndCurrentCounts(t *testing.T) {
	ctx := context.Background()

	rdb, mock := redismock.NewClientMock()
	cm := NewConcurrencyManager(rdb)
	mock.Regexp().ExpectEvalSha(acquireSlotScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		`^\d+$`, "2", "req", "300",
	).SetErr(errors.New("redis down"))
	if err := cm.AcquireSlot(ctx, 7, "req", 2, 0); err != nil {
		t.Fatalf("AcquireSlot redis error should fail open, got %v", err)
	}
	mock.Regexp().ExpectEvalSha(acquireSlotScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		`^\d+$`, "2", "bad", "300",
	).SetVal("bad")
	if err := cm.AcquireSlot(ctx, 7, "bad", 2, 0); err != nil {
		t.Fatalf("AcquireSlot malformed result should fail open, got %v", err)
	}
	mock.ExpectEvalSha(releaseSlotScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		"req", int(defaultSlotTTL.Seconds()), int(concurrencyZeroCountTTL.Seconds()),
	).SetErr(errors.New("redis down"))
	cm.ReleaseSlot(ctx, 7, "req")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	cm = NewConcurrencyManager(rdb)
	mock.ExpectMGet(concurrencyCountKey(7)).SetVal([]interface{}{"5"})
	if got := cm.GetCurrentCount(ctx, 7); got != 5 {
		t.Fatalf("GetCurrentCount = %d, want 5", got)
	}
	mock.ExpectMGet(concurrencyCountKey(7), concurrencyCountKey(8)).SetVal([]interface{}{"1", []byte("2")})
	got := cm.GetCurrentCounts(ctx, []int{7, 8, 7})
	if got[7] != 1 || got[8] != 2 || len(got) != 2 {
		t.Fatalf("GetCurrentCounts = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	mock.ExpectMGet(concurrencyCountKey(7), concurrencyCountKey(8)).SetVal([]interface{}{nil, "2"})
	mock.Regexp().ExpectEvalSha(backfillConcurrencyCountsScript.Hash(), []string{concurrencyKey(7), concurrencyCountKey(7)},
		`^\d+$`, "300", "30",
	).SetVal([]interface{}{int64(4)})
	backfilled := loadConcurrencyCounts(ctx, rdb, []int{7, 8}, true)
	if backfilled[7] != 4 || backfilled[8] != 2 || len(backfilled) != 2 {
		t.Fatalf("loadConcurrencyCounts backfilled = %#v", backfilled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	mock.ExpectMGet(concurrencyCountKey(7)).SetErr(errors.New("mget failed"))
	if got := loadConcurrencyCounts(ctx, rdb, []int{7}, true); len(got) != 0 {
		t.Fatalf("loadConcurrencyCounts mget error = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestSessionManagerNilAndRedisScripts(t *testing.T) {
	ctx := context.Background()
	sm := NewSessionManager(nil)
	if sm == nil || sm.rdb != nil {
		t.Fatalf("NewSessionManager(nil) = %#v", sm)
	}
	if got := sessionLimitKey(7); got != "ag:session:limit:7" {
		t.Fatalf("sessionLimitKey = %q", got)
	}
	if ok, err := sm.RegisterSession(ctx, 7, "sess", 1, time.Minute); err != nil || !ok {
		t.Fatalf("RegisterSession nil redis = %v, %v", ok, err)
	}
	if err := sm.RefreshSession(ctx, 7, "sess", time.Minute); err != nil {
		t.Fatalf("RefreshSession nil redis = %v", err)
	}
	if got, err := sm.GetActiveSessionCount(ctx, 7, time.Minute); err != nil || got != 0 {
		t.Fatalf("GetActiveSessionCount nil redis = %d, %v", got, err)
	}
	if ok, err := sm.IsSessionActive(ctx, 7, "sess", time.Minute); err != nil || !ok {
		t.Fatalf("IsSessionActive nil redis = %v, %v", ok, err)
	}
	if got := sm.GetSchedulability(ctx, 7, map[string]interface{}{"max_sessions": 0}); got != Normal {
		t.Fatalf("GetSchedulability unlimited = %v", got)
	}
	if got := sm.GetSchedulabilityBatch(ctx, []*ent.Account{{ID: 7, Extra: map[string]interface{}{"max_sessions": 1}}}); len(got) != 0 {
		t.Fatalf("GetSchedulabilityBatch nil redis = %#v", got)
	}

	rdb, mock := redismock.NewClientMock()
	sm = NewSessionManager(rdb)
	mock.ExpectEvalSha(registerSessionScript.Hash(), []string{sessionLimitKey(7)}, "sess", 2, 60).SetVal(int64(1))
	if ok, err := sm.RegisterSession(ctx, 7, "sess", 2, time.Minute); err != nil || !ok {
		t.Fatalf("RegisterSession allowed = %v, %v", ok, err)
	}
	mock.ExpectEvalSha(registerSessionScript.Hash(), []string{sessionLimitKey(7)}, "sess-2", 2, 60).SetVal(int64(0))
	if ok, err := sm.RegisterSession(ctx, 7, "sess-2", 2, time.Minute); err != nil || ok {
		t.Fatalf("RegisterSession denied = %v, %v", ok, err)
	}
	mock.ExpectEvalSha(registerSessionScript.Hash(), []string{sessionLimitKey(7)}, "fail", 2, 60).SetErr(errors.New("redis down"))
	if ok, err := sm.RegisterSession(ctx, 7, "fail", 2, time.Minute); err != nil || !ok {
		t.Fatalf("RegisterSession error should fail open = %v, %v", ok, err)
	}
	mock.ExpectEvalSha(refreshSessionScript.Hash(), []string{sessionLimitKey(7)}, "sess", 60).SetVal(int64(1))
	if err := sm.RefreshSession(ctx, 7, "sess", time.Minute); err != nil {
		t.Fatalf("RefreshSession = %v", err)
	}
	mock.ExpectEvalSha(refreshSessionScript.Hash(), []string{sessionLimitKey(7)}, "fail", 60).SetErr(errors.New("refresh failed"))
	if err := sm.RefreshSession(ctx, 7, "fail", time.Minute); err == nil {
		t.Fatal("RefreshSession error = nil")
	}
	mock.ExpectEvalSha(getActiveSessionCountScript.Hash(), []string{sessionLimitKey(7)}, 60).SetVal(int64(3))
	if got, err := sm.GetActiveSessionCount(ctx, 7, time.Minute); err != nil || got != 3 {
		t.Fatalf("GetActiveSessionCount = %d, %v", got, err)
	}
	mock.ExpectEvalSha(getActiveSessionCountScript.Hash(), []string{sessionLimitKey(7)}, 60).SetErr(errors.New("count failed"))
	if _, err := sm.GetActiveSessionCount(ctx, 7, time.Minute); err == nil {
		t.Fatal("GetActiveSessionCount error = nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestSessionManagerActivitySchedulabilityAndBatch(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	sm := NewSessionManager(rdb)

	mock.ExpectZScore(sessionLimitKey(7), "active").SetVal(float64(time.Now().Unix()))
	if ok, err := sm.IsSessionActive(ctx, 7, "active", time.Minute); err != nil || !ok {
		t.Fatalf("IsSessionActive active = %v, %v", ok, err)
	}
	mock.ExpectZScore(sessionLimitKey(7), "expired").SetVal(float64(time.Now().Add(-2 * time.Minute).Unix()))
	if ok, err := sm.IsSessionActive(ctx, 7, "expired", time.Minute); err != nil || ok {
		t.Fatalf("IsSessionActive expired = %v, %v", ok, err)
	}
	mock.ExpectZScore(sessionLimitKey(7), "missing").SetErr(redis.Nil)
	if ok, err := sm.IsSessionActive(ctx, 7, "missing", time.Minute); err != nil || ok {
		t.Fatalf("IsSessionActive missing = %v, %v", ok, err)
	}
	mock.ExpectZScore(sessionLimitKey(7), "fail").SetErr(errors.New("zscore failed"))
	if ok, err := sm.IsSessionActive(ctx, 7, "fail", time.Minute); err == nil || !ok {
		t.Fatalf("IsSessionActive error = %v, %v", ok, err)
	}

	mock.ExpectEvalSha(getActiveSessionCountScript.Hash(), []string{sessionLimitKey(7)}, 60).SetVal(int64(2))
	if got := sm.GetSchedulability(ctx, 7, map[string]interface{}{"max_sessions": 2, "session_idle_timeout": 60}); got != StickyOnly {
		t.Fatalf("GetSchedulability full = %v", got)
	}
	mock.ExpectEvalSha(getActiveSessionCountScript.Hash(), []string{sessionLimitKey(7)}, int(defaultSessionIdleTimeout.Seconds())).SetErr(errors.New("count failed"))
	if got := sm.GetSchedulability(ctx, 7, map[string]interface{}{"max_sessions": 2}); got != Normal {
		t.Fatalf("GetSchedulability fail open = %v", got)
	}

	accounts := []*ent.Account{
		nil,
		{ID: 7, Extra: map[string]interface{}{"max_sessions": 2, "session_idle_timeout": 30}},
		{ID: 8, Extra: map[string]interface{}{"max_sessions": 1}},
		{ID: 9, Extra: map[string]interface{}{"max_sessions": 0}},
	}
	mock.ExpectEvalSha(getActiveSessionCountsScript.Hash(), []string{sessionLimitKey(7), sessionLimitKey(8)},
		30, int(defaultSessionIdleTimeout.Seconds()),
	).SetVal([]interface{}{int64(1), int64(1), int64(99)})
	got := sm.GetSchedulabilityBatch(ctx, accounts)
	if len(got) != 1 || got[8] != StickyOnly {
		t.Fatalf("GetSchedulabilityBatch = %#v", got)
	}

	mock.ExpectEvalSha(getActiveSessionCountsScript.Hash(), []string{sessionLimitKey(7)},
		int(defaultSessionIdleTimeout.Seconds()),
	).SetVal([]interface{}{"bad"})
	if got := sm.GetSchedulabilityBatch(ctx, []*ent.Account{{ID: 7, Extra: map[string]interface{}{"max_sessions": 1}}}); len(got) != 0 {
		t.Fatalf("GetSchedulabilityBatch bad value = %#v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
