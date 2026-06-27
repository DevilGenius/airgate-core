package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

func TestMessageQueueDefaultTTLAndWaiterReleaseBranches(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	queue := NewMessageQueue(rdb, NewRPMCounter(nil))
	lockKey := msgQueueLockKey(7)

	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "default", defaultLockTTL.Milliseconds()).SetVal(int64(1))
	if ok, err := queue.TryAcquire(ctx, 7, "default", 0); err != nil || !ok {
		t.Fatalf("TryAcquire default ttl = %v, %v", ok, err)
	}
	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "wait-default", defaultLockTTL.Milliseconds()).SetVal(int64(1))
	if ok, err := queue.WaitAcquire(ctx, 7, "wait-default", 0, 0); err != nil || !ok {
		t.Fatalf("WaitAcquire default ttl = %v, %v", ok, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	queue = NewMessageQueue(rdb, NewRPMCounter(nil))
	waiterKey := waitersCounterKey(7)
	waiterIndexKey := waitersIndexKey()
	waiterTTL := time.Second + time.Second + 60*time.Second
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "cancel", int64(1000)).SetVal(int64(0))
	mock.ExpectEvalSha(registerWaiterScript.Hash(), []string{waiterKey, waiterIndexKey}, 2, waiterTTL.Milliseconds(), 7).SetVal([]interface{}{int64(1), int64(1)})
	mock.ExpectTTL(lockKey).SetVal(time.Second)
	mock.ExpectEvalSha(releaseWaiterScript.Hash(), []string{waiterKey, waiterIndexKey}, waiterTTL.Milliseconds(), 7).SetVal(int64(1))
	if ok, err := queue.WaitAcquire(cancelled, 7, "cancel", time.Second, time.Second, 2); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitAcquire cancelled = %v, %v", ok, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations after cancel: %v", err)
	}
}

func TestMessageQueueWaitAcquireRetriesAfterShortTTL(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	queue := NewMessageQueue(rdb, NewRPMCounter(nil))
	lockKey := msgQueueLockKey(9)
	waiterKey := waitersCounterKey(9)
	waiterIndexKey := waitersIndexKey()
	waiterTTL := time.Second + time.Second + 60*time.Second

	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "retry", int64(1000)).SetVal(int64(0))
	mock.ExpectEvalSha(registerWaiterScript.Hash(), []string{waiterKey, waiterIndexKey}, 3, waiterTTL.Milliseconds(), 9).SetVal([]interface{}{int64(1), int64(1)})
	mock.ExpectTTL(lockKey).SetVal(time.Nanosecond)
	mock.ExpectEvalSha(acquireLockScript.Hash(), []string{lockKey}, "retry", int64(1000)).SetVal(int64(1))
	mock.ExpectEvalSha(releaseWaiterScript.Hash(), []string{waiterKey, waiterIndexKey}, waiterTTL.Milliseconds(), 9).SetVal(int64(1))

	if ok, err := queue.WaitAcquire(ctx, 9, "retry", time.Second, time.Second, 3); err != nil || !ok {
		t.Fatalf("WaitAcquire retry = %v, %v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestMessageQueueCalculateDelayBranches(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	queue := NewMessageQueue(rdb, NewRPMCounter(rdb))
	minuteTime := time.Unix(120, 0)
	key := rpmMinuteKey(7, 2)

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetErr(errors.New("rpm unavailable"))
	if got := queue.CalculateDelay(ctx, 7, 60); got != defaultMinDelay {
		t.Fatalf("CalculateDelay error = %s, want %s", got, defaultMinDelay)
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetVal("10")
	if got := queue.CalculateDelay(ctx, 7, 60); got < defaultMinDelay-(defaultMinDelay/5) || got > defaultMinDelay+(defaultMinDelay/5) {
		t.Fatalf("CalculateDelay low rpm = %s", got)
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetVal("40")
	if got := queue.CalculateDelay(ctx, 7, 60); got <= defaultMinDelay || got >= defaultMaxDelay {
		t.Fatalf("CalculateDelay mid rpm = %s", got)
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetVal("60")
	if got := queue.CalculateDelay(ctx, 7, 60); got <= time.Second {
		t.Fatalf("CalculateDelay high rpm = %s", got)
	}

	mock.ExpectTime().SetVal(minuteTime)
	mock.ExpectGet(key).SetVal("0")
	mock.ExpectEvalSha(enforceDelayElapsedScript.Hash(), []string{msgQueueLastKey(7)}).SetErr(redis.Nil)
	if err := queue.EnforceDelay(ctx, 7, 60); err != nil {
		t.Fatalf("EnforceDelay timer branch = %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
