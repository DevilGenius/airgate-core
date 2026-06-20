package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
)

func TestFamilyCooldownRedisPaths(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	fc := NewFamilyCooldown(rdb)
	until := time.UnixMilli(12345)
	indexKey := familyCooldownIndexKey(7)
	activeKey := familyCooldownActiveKey(7)
	reasonKey := familyCooldownReasonKey(7, "gpt")

	mock.ExpectSet(reasonKey, "429", time.Millisecond).SetVal("OK")
	mock.ExpectZAdd(indexKey, redis.Z{Score: float64(until.UnixMilli()), Member: "gpt"}).SetVal(1)
	mock.ExpectEval(familyCooldownIndexExpireScript, []string{indexKey}, familyCooldownIndexTTL.Milliseconds()).SetVal(int64(1))
	mock.ExpectSetNX(activeKey, "1", 0).SetVal(true)
	mock.ExpectEval(familyCooldownIndexExpireScript, []string{activeKey}, (time.Minute + time.Millisecond).Milliseconds()).SetVal(int64(1))
	fc.Mark(ctx, 7, "gpt", until, "429")

	mock.ExpectTTL(reasonKey).SetVal(time.Minute)
	if gotUntil, ok := fc.Until(ctx, 7, "gpt"); !ok || !gotUntil.After(time.Now()) {
		t.Fatalf("Until active = %v, %v", gotUntil, ok)
	}
	mock.ExpectTTL(reasonKey).SetVal(0)
	if _, ok := fc.Until(ctx, 7, "gpt"); ok {
		t.Fatal("Until zero ttl ok = true")
	}
	mock.ExpectTTL(reasonKey).SetErr(errors.New("ttl failed"))
	if _, ok := fc.Until(ctx, 7, "gpt"); ok {
		t.Fatal("Until error ok = true")
	}

	mock.ExpectMGet(familyCooldownReasonKey(7, "gpt"), familyCooldownReasonKey(8, "gpt")).SetVal([]interface{}{nil, "429"})
	inCooldown := fc.InCooldownBatch(ctx, []int{7, 8, 7}, "gpt")
	if len(inCooldown) != 1 || !inCooldown[8] {
		t.Fatalf("InCooldownBatch = %#v", inCooldown)
	}
	mock.ExpectMGet(familyCooldownReasonKey(7, "gpt")).SetErr(errors.New("mget failed"))
	if got := fc.InCooldownBatch(ctx, []int{7}, "gpt"); len(got) != 0 {
		t.Fatalf("InCooldownBatch error = %#v", got)
	}

	mock.ExpectDel(reasonKey).SetVal(1)
	mock.ExpectZRem(indexKey, "gpt").SetVal(1)
	fc.Clear(ctx, 7, "gpt")

	mock.ExpectZRange(indexKey, 0, -1).SetVal([]string{"gpt", "img"})
	mock.ExpectDel(familyCooldownReasonKey(7, "gpt"), familyCooldownReasonKey(7, "img")).SetVal(2)
	mock.ExpectDel(indexKey).SetVal(1)
	mock.ExpectDel(activeKey).SetVal(1)
	if got := fc.ClearAccount(ctx, 7); got != 2 {
		t.Fatalf("ClearAccount = %d, want 2", got)
	}
	mock.ExpectZRange(indexKey, 0, -1).SetErr(errors.New("zrange failed"))
	if got := fc.ClearAccount(ctx, 7); got != 0 {
		t.Fatalf("ClearAccount error = %d", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestFamilyCooldownListBatchRedisPaths(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	fc := NewFamilyCooldown(rdb)
	until := time.Now().Add(time.Minute)

	mock.ExpectMGet(familyCooldownActiveKey(7), familyCooldownActiveKey(8)).SetVal([]interface{}{"1", nil})
	mock.Regexp().ExpectEvalSha(listFamilyCooldownsScript.Hash(), []string{familyCooldownIndexKey(7)}, `^\d+$`).
		SetVal([]interface{}{
			int64(1), "gpt", float64(until.UnixMilli()),
			int64(1), "stale", float64(until.Add(time.Second).UnixMilli()),
			int64(2), "ignored", float64(until.UnixMilli()),
			int64(1), "", float64(until.UnixMilli()),
			int64(1), "bad-score", "bad",
		})
	mock.ExpectGet(familyCooldownReasonKey(7, "gpt")).SetVal("429")
	mock.ExpectGet(familyCooldownReasonKey(7, "stale")).SetErr(redis.Nil)
	mock.ExpectZRem(familyCooldownIndexKey(7), "stale").SetVal(1)
	got := fc.ListBatch(ctx, []int{7, 8, 7})
	if len(got[7]) != 1 || got[7][0].Family != "gpt" || got[7][0].Reason != "429" {
		t.Fatalf("ListBatch = %#v", got)
	}

	mock.ExpectMGet(familyCooldownActiveKey(7)).SetVal([]interface{}{nil})
	if got := fc.ListBatch(ctx, []int{7}); len(got) != 0 {
		t.Fatalf("ListBatch no active = %#v", got)
	}
	mock.ExpectMGet(familyCooldownActiveKey(7)).SetErr(errors.New("mget failed"))
	if got := fc.activeFamilyCooldownAccountIDs(ctx, []int{7}); got != nil {
		t.Fatalf("activeFamilyCooldownAccountIDs error = %#v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRPMCounterRedisPaths(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	rpm := NewRPMCounter(rdb)
	minuteTime := time.Unix(120, 0)
	key := rpmMinuteKey(7, 2)

	mock.ExpectTime().SetVal(minuteTime)
	if got := rpm.currentMinute(ctx); got != 2 {
		t.Fatalf("currentMinute = %d, want 2", got)
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectTxPipeline()
	mock.ExpectIncr(key).SetVal(3)
	mock.ExpectExpire(key, rpmKeyTTL).SetVal(true)
	mock.ExpectTxPipelineExec()
	if got, err := rpm.IncrementRPM(ctx, 7); err != nil || got != 3 {
		t.Fatalf("IncrementRPM = %d, %v", got, err)
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetVal("4")
	if got, err := rpm.GetRPM(ctx, 7); err != nil || got != 4 {
		t.Fatalf("GetRPM = %d, %v", got, err)
	}
	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetErr(redis.Nil)
	if got, err := rpm.GetRPM(ctx, 7); err != nil || got != 0 {
		t.Fatalf("GetRPM nil = %d, %v", got, err)
	}
	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetErr(errors.New("get failed"))
	if _, err := rpm.GetRPM(ctx, 7); err == nil {
		t.Fatal("GetRPM error = nil")
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectEvalSha(decrementRPMScript.Hash(), []string{key}).SetVal(int64(1))
	rpm.DecrementRPM(ctx, 7)

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectEvalSha(tryIncrementScript.Hash(), []string{key}, 10).SetVal(int64(-1))
	if ok, err := rpm.TryIncrementRPM(ctx, 7, 10); err != nil || ok {
		t.Fatalf("TryIncrementRPM full = %v, %v", ok, err)
	}
	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectEvalSha(tryIncrementScript.Hash(), []string{key}, 10).SetVal(int64(5))
	if ok, err := rpm.TryIncrementRPM(ctx, 7, 10); err != nil || !ok {
		t.Fatalf("TryIncrementRPM allowed = %v, %v", ok, err)
	}
	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectTxPipeline()
	mock.ExpectIncr(key).SetVal(6)
	mock.ExpectExpire(key, rpmKeyTTL).SetVal(true)
	mock.ExpectTxPipelineExec()
	if ok, err := rpm.TryIncrementRPM(ctx, 7, 0); err != nil || !ok {
		t.Fatalf("TryIncrementRPM unlimited = %v, %v", ok, err)
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectEvalSha(tryIncrementScript.Hash(), []string{key}, 10).SetErr(errors.New("eval failed"))
	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectTxPipeline()
	mock.ExpectIncr(key).SetErr(errors.New("incr failed"))
	if ok, err := rpm.TryIncrementRPM(ctx, 7, 10); err != nil || !ok {
		t.Fatalf("TryIncrementRPM fail open = %v, %v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations before batch: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	rpm = NewRPMCounter(rdb)
	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectMGet(rpmMinuteKey(7, 2), rpmMinuteKey(8, 2)).SetVal([]interface{}{"8", "10"})
	batch := rpm.GetSchedulabilityBatch(ctx, []*ent.Account{
		nil,
		{ID: 7, Extra: map[string]interface{}{"max_rpm": 10}},
		{ID: 8, Extra: map[string]interface{}{"max_rpm": 10}},
		{ID: 9, Extra: map[string]interface{}{"max_rpm": 0}},
	})
	if batch[7] != StickyOnly || batch[8] != NotSchedulable || len(batch) != 2 {
		t.Fatalf("GetSchedulabilityBatch = %#v", batch)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestMessageQueueRedisPaths(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	queue := NewMessageQueue(rdb, NewRPMCounter(nil))
	lockKey := msgQueueLockKey(7)
	lastKey := msgQueueLastKey(7)

	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "req", int64(1000)).SetVal(int64(1))
	if ok, err := queue.TryAcquire(ctx, 7, "req", time.Second); err != nil || !ok {
		t.Fatalf("TryAcquire allowed = %v, %v", ok, err)
	}
	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "busy", int64(1000)).SetVal(int64(0))
	if ok, err := queue.TryAcquire(ctx, 7, "busy", time.Second); err != nil || ok {
		t.Fatalf("TryAcquire busy = %v, %v", ok, err)
	}
	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "fail", int64(1000)).SetErr(errors.New("eval failed"))
	if ok, err := queue.TryAcquire(ctx, 7, "fail", time.Second); err != nil || !ok {
		t.Fatalf("TryAcquire fail open = %v, %v", ok, err)
	}

	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "nowait", int64(1000)).SetVal(int64(1))
	if ok, err := queue.WaitAcquire(ctx, 7, "nowait", time.Second, 0); err != nil || !ok {
		t.Fatalf("WaitAcquire no timeout = %v, %v", ok, err)
	}

	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "queued", int64(1000)).SetVal(int64(0))
	mock.ExpectEvalSha(registerWaiterScript.Hash(), []string{waitersCounterKey(7)}, 1, int64(62000)).SetVal(int64(0))
	if ok, err := queue.WaitAcquire(ctx, 7, "queued", time.Second, time.Second, 1); err != nil || ok {
		t.Fatalf("WaitAcquire full queue = %v, %v", ok, err)
	}

	mock.ExpectDel(lockKey).SetVal(1)
	if err := queue.ForceRelease(ctx, 7); err != nil {
		t.Fatalf("ForceRelease = %v", err)
	}
	mock.ExpectEvalSha(releaseLockScript.Hash(), []string{lockKey, lastKey}, "req").SetVal(int64(1))
	if err := queue.Release(ctx, 7, "req"); err != nil {
		t.Fatalf("Release = %v", err)
	}
	mock.ExpectEvalSha(enforceDelayElapsedScript.Hash(), []string{lastKey}).SetVal(int64(9999))
	if err := queue.EnforceDelay(ctx, 7, 60); err != nil {
		t.Fatalf("EnforceDelay elapsed = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestWindowCostRedisAndStateCachePaths(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	checker := NewWindowCostChecker(nil, rdb)

	mock.ExpectGet(windowCostKey(7)).SetVal("12.5")
	if got, err := checker.GetWindowCost(ctx, 7, 5); err != nil || got != 12.5 {
		t.Fatalf("GetWindowCost cache = %v, %v", got, err)
	}
	mock.ExpectGet(windowCostKey(7)).SetVal("90")
	if got := checker.GetSchedulability(ctx, 7, map[string]interface{}{"max_window_cost": 100}); got != StickyOnly {
		t.Fatalf("GetSchedulability cache = %v", got)
	}
	mock.ExpectMGet(windowCostKey(7), windowCostKey(8), windowCostKey(9)).SetVal([]interface{}{"50", "110", nil})
	batch := checker.GetSchedulabilityBatch(ctx, []*ent.Account{
		nil,
		{ID: 7, Extra: map[string]interface{}{"max_window_cost": 100}},
		{ID: 8, Extra: map[string]interface{}{"max_window_cost": 100}},
		{ID: 9, Extra: map[string]interface{}{"max_window_cost": 100}},
		{ID: 10, Extra: map[string]interface{}{"max_window_cost": 0}},
	})
	if batch[7] != Normal || batch[8] != NotSchedulable || len(batch) != 2 {
		t.Fatalf("GetSchedulabilityBatch cache = %#v", batch)
	}
	mock.ExpectEvalSha(addCostScript.Hash(), []string{windowCostKey(7)}, 1.5).SetVal("13.5")
	checker.AddCost(ctx, 7, 1.5)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	cache := newAccountStateCache()
	var nilCache *accountStateCache
	nilCache.Store(7, account.StateActive, nil, nil)
	nilCache.Delete(7)
	if got := nilCache.Apply(nil); got != nil {
		t.Fatalf("nil cache Apply nil = %#v", got)
	}
	until := time.Now().Add(time.Minute)
	extra := map[string]interface{}{"step": 2}
	cache.Store(7, account.StateDegraded, &until, extra)
	extra["step"] = 3
	acc := &ent.Account{ID: 7, State: account.StateActive, Extra: map[string]interface{}{"old": true}}
	applied := cache.Apply(acc)
	if applied == acc || applied.State != account.StateDegraded || applied.StateUntil == nil ||
		applied.Extra["step"] != 2 {
		t.Fatalf("Apply cached state = %#v", applied)
	}
	cache.Delete(7)
	if got := cache.Apply(acc); got != acc {
		t.Fatalf("Apply after delete returned clone: %#v", got)
	}
	cache.Store(0, account.StateDisabled, nil, nil)
	cache.Delete(0)
}
