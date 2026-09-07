package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLocalClientLimitsSharedAcrossSchedulers(t *testing.T) {
	rdbA, rdbB := localReviewRedis(t), localReviewRedis(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	now, err := rdbA.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	if now.Second() >= 59 {
		t.Skip("avoid crossing a real Redis minute boundary")
	}
	id := -int(time.Now().UnixNano()) // Real entity IDs are positive; tests cannot collide.
	defer rdbA.Del(context.Background(), clientRPMKey(id), userConcurrencyKey(id), userConcurrencyCountKey(id), userConcurrencyKey(id)+":leases", apiKeyConcurrencyKey(id), apiKeyConcurrencyCountKey(id), apiKeyConcurrencyKey(id)+":leases")
	a, b := NewScheduler(nil, rdbA), NewScheduler(nil, rdbB)
	for _, s := range []*Scheduler{a, b} {
		if ok, err := s.AllowAPIKeyRPM(ctx, id, 5, 2, true); err != nil || !ok {
			t.Fatalf("admit = %v %v", ok, err)
		}
	}
	if ok, err := a.AllowAPIKeyRPM(ctx, id, 5, 2, true); err != nil || ok {
		t.Fatal("non-Responses limit multiplied by instances")
	}
	for _, s := range []*Scheduler{b, a, b} {
		if ok, err := s.AllowAPIKeyRPM(ctx, id, 5, 2, false); err != nil || !ok {
			t.Fatal("Responses budget incorrectly consumed")
		}
	}
	if ok, err := a.AllowAPIKeyRPM(ctx, id, 5, 2, false); err != nil || ok {
		t.Fatal("total RPM multiplied by instances")
	}
	ca, cb := NewConcurrencyManager(rdbA), NewConcurrencyManager(rdbB)
	if err := ca.AcquireUserSlot(ctx, id, "first", 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cb.AcquireUserSlot(ctx, id, "second", 1, time.Minute); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatal("user concurrency multiplied by instances")
	}
	ca.ReleaseUserSlot(ctx, id, "first")
	if err := cb.AcquireUserSlot(ctx, id, "second", 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := ca.AcquireAPIKeySlot(ctx, id, "first", 1, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cb.AcquireAPIKeySlot(ctx, id, "second", 1, time.Minute); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatal("key concurrency multiplied by instances")
	}
}
