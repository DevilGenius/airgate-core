package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestLeaseLossCancelsExecutionAndStopJoinsRenewal(t *testing.T) {
	var calls atomic.Int64
	ctx, stop := MaintainLease(t.Context(), 60*time.Millisecond, func(context.Context) (bool, error) { return calls.Add(1) < 3, nil })
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("lost lease did not cancel execution")
	}
	if !errors.Is(context.Cause(ctx), ErrLeaseLost) {
		t.Fatal(context.Cause(ctx))
	}
	stop()
	if calls.Load() != 3 {
		t.Fatal("renewal continued after loss")
	}
}

func TestLocalSlotExpiryUsesOwnerDeadlineAndNeverRevivesLostToken(t *testing.T) {
	rdb := localReviewRedis(t)
	cm := NewConcurrencyManager(rdb)
	key := "ag:review:r13:" + uuid.NewString()
	countKey := key + ":count"
	defer rdb.Del(context.Background(), key, countKey, key+":leases")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	acquire := func(token string, ttl time.Duration) error {
		_, _, err := cm.acquireSlotByKey(ctx, key, countKey, "", "", token, 1, ttl)
		return err
	}
	if err := acquire("first", time.Minute); err != nil {
		t.Fatal(err)
	}
	// A long-running owner may have an old score. Its per-token deadline wins
	// over a new caller's much shorter TTL, and renewal restores its heartbeat.
	if err := rdb.ZAdd(ctx, key, redis.Z{Member: "first", Score: float64(time.Now().Add(-10 * time.Minute).Unix())}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := acquire("second", time.Second); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("active long lease was purged: %v", err)
	}
	if owned, err := cm.renewSlotByKey(ctx, key, countKey, "first", time.Minute); err != nil || !owned {
		t.Fatalf("renewal=%v %v", owned, err)
	}
	if err := rdb.ZAdd(ctx, key+":leases", redis.Z{Member: "first", Score: float64(time.Now().Add(-time.Second).Unix())}).Err(); err != nil {
		t.Fatal(err)
	}
	if owned, err := cm.renewSlotByKey(ctx, key, countKey, "first", time.Minute); err != nil || owned {
		t.Fatalf("expired owner revived: %v %v", owned, err)
	}
	if err := acquire("second", time.Minute); err != nil {
		t.Fatal(err)
	}
	cm.releaseSlotByKey(ctx, key, countKey, "", "", "first")
	if count, err := rdb.ZCard(ctx, key).Result(); err != nil || count != 1 {
		t.Fatal("late release removed new owner")
	}
	if owned, err := cm.renewSlotByKey(ctx, key, countKey, "second", time.Minute); err != nil || !owned {
		t.Fatal("current owner lost")
	}
}

func TestLocalMessageRenewalChecksOwnership(t *testing.T) {
	rdb := localReviewRedis(t)
	key := "ag:review:r13:message:" + uuid.NewString()
	ctx := t.Context()
	defer rdb.Del(context.Background(), key)
	if err := rdb.Set(ctx, key, "new", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if result, err := renewMessageLockScript.Run(ctx, rdb, []string{key}, "old", int64(60000)).Int(); err != nil || result != 0 {
		t.Fatal("old owner renewed lock")
	}
	if result, err := renewMessageLockScript.Run(ctx, rdb, []string{key}, "new", int64(60000)).Int(); err != nil || result != 1 {
		t.Fatal("new owner could not renew lock")
	}
}
