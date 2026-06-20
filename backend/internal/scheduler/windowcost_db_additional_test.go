package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/ent"
)

func TestWindowCostCheckerDatabaseAggregation(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_window_cost_db")
	acc1 := createStateMachineAccount(ctx, db, "cost one", false)
	acc2 := createStateMachineAccount(ctx, db, "cost two", false)
	now := time.Now()

	createWindowCostUsageLog(t, db, acc1.ID, "recent-1", 1.25, now.Add(-time.Hour))
	createWindowCostUsageLog(t, db, acc1.ID, "recent-2", 2.75, now.Add(-2*time.Hour))
	createWindowCostUsageLog(t, db, acc1.ID, "old", 99, now.Add(-10*time.Hour))
	createWindowCostUsageLog(t, db, acc2.ID, "recent-3", 81, now.Add(-time.Hour))

	checker := NewWindowCostChecker(db, nil)
	cost, err := checker.GetWindowCost(ctx, acc1.ID, 5)
	if err != nil {
		t.Fatalf("GetWindowCost() error = %v", err)
	}
	if cost != 4 {
		t.Fatalf("GetWindowCost() = %v, want 4", cost)
	}

	costs, err := checker.loadWindowCosts(ctx, []int{acc1.ID, acc2.ID, 99999}, 5)
	if err != nil {
		t.Fatalf("loadWindowCosts() error = %v", err)
	}
	if costs[acc1.ID] != 4 || costs[acc2.ID] != 81 || costs[99999] != 0 {
		t.Fatalf("loadWindowCosts() = %+v", costs)
	}
	empty, err := checker.loadWindowCosts(ctx, nil, 5)
	if err != nil || len(empty) != 0 {
		t.Fatalf("loadWindowCosts(nil) = %+v/%v", empty, err)
	}

	got := checker.GetSchedulabilityBatch(ctx, []*ent.Account{
		{ID: acc1.ID, Extra: map[string]interface{}{"max_window_cost": 10.0, "sticky_reserve": 2.0}},
		{ID: acc2.ID, Extra: map[string]interface{}{"max_window_cost": 100.0, "sticky_reserve": 10.0}},
		{ID: 99999, Extra: map[string]interface{}{"max_window_cost": 1.0}},
		nil,
		{ID: 100000},
	})
	if got[acc1.ID] != Normal || got[acc2.ID] != StickyOnly || got[99999] != Normal {
		t.Fatalf("GetSchedulabilityBatch() = %+v", got)
	}
}

func TestWindowCostCheckerRedisMissWritesDatabaseCost(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_window_cost_redis_miss")
	acc := createStateMachineAccount(ctx, db, "redis miss", false)
	createWindowCostUsageLog(t, db, acc.ID, "recent", 3.5, time.Now().Add(-time.Hour))

	rdb, mock := redismock.NewClientMock()
	checker := NewWindowCostChecker(db, rdb)
	mock.ExpectGet(windowCostKey(acc.ID)).SetErr(redis.Nil)
	mock.ExpectSet(windowCostKey(acc.ID), "3.50000000", windowCostCacheTTL).SetVal("OK")
	cost, err := checker.GetWindowCost(ctx, acc.ID, 5)
	if err != nil {
		t.Fatalf("GetWindowCost(redis miss) error = %v", err)
	}
	if cost != 3.5 {
		t.Fatalf("GetWindowCost(redis miss) = %v, want 3.5", cost)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func createWindowCostUsageLog(t *testing.T, db *ent.Client, accountID int, eventID string, actualCost float64, createdAt time.Time) {
	t.Helper()
	if _, err := db.UsageLog.Create().
		SetBillingEventID(eventID).
		SetPlatform("openai").
		SetModel("gpt-5").
		SetAccountID(accountID).
		SetActualCost(actualCost).
		SetCreatedAt(createdAt).
		Save(context.Background()); err != nil {
		t.Fatalf("create usage log %s: %v", eventID, err)
	}
}
