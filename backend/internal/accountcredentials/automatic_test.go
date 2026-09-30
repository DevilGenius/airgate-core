package accountcredentials

import (
	"context"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestAutomaticPlanLock(t *testing.T) {
	for _, test := range []struct {
		name    string
		locked  bool
		plan    string
		hasPlan bool
	}{
		{"unlocked", false, "plus", true},
		{"locked", true, "team", true},
		{"locked_empty", true, "", true},
		{"locked_missing", true, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := testdb.OpenMemoryEnt(t, t.Name())
			defer func() { _ = db.Close() }()
			credentials := map[string]string{"access_token": "old", "refresh_token": "keep"}
			if test.hasPlan {
				credentials["plan_type"] = test.plan
			}
			item := db.Account.Create().SetName(test.name).SetPlatform("openai").SetCredentials(credentials).SetExtra(map[string]any{PlanTypeLockedKey: test.locked}).SaveX(t.Context())
			for _, nextPlan := range []string{"pro", ""} {
				updated, err := UpdateAutomatic(t.Context(), db, item.ID, map[string]string{"access_token": "new", "plan_type": nextPlan})
				if err != nil {
					t.Fatal(err)
				}
				want, present := nextPlan, true
				if test.locked {
					want, present = test.plan, test.hasPlan
				}
				got, exists := updated.Credentials["plan_type"]
				if got != want || exists != present || updated.Credentials["access_token"] != "new" || updated.Credentials["refresh_token"] != "keep" {
					t.Fatalf("unexpected credentials: %#v", updated.Credentials)
				}
			}
		})
	}
}

func TestAutomaticPlanLockConcurrentManualEdit(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, t.Name())
	defer func() { _ = db.Close() }()
	item := db.Account.Create().SetName("race").SetPlatform("openai").SetCredentials(map[string]string{"plan_type": "plus", "access_token": "old"}).SaveX(t.Context())
	interfered := false
	db.Account.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if !interfered {
				interfered = true
				_, err := db.Account.UpdateOneID(item.ID).SetUpdatedAt(item.UpdatedAt.Add(time.Second)).SetExtra(map[string]any{PlanTypeLockedKey: true}).SetCredentials(map[string]string{"plan_type": "team", "access_token": "old"}).Save(ctx)
				if err != nil {
					return nil, err
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	updated, err := UpdateAutomatic(t.Context(), db, item.ID, map[string]string{"plan_type": "pro", "access_token": "new"})
	if err != nil {
		t.Fatal(err)
	}
	if !interfered || updated.Credentials["plan_type"] != "team" || updated.Credentials["access_token"] != "new" || !PlanTypeLocked(updated.Extra) {
		t.Fatalf("concurrent lock lost: %+v", updated)
	}
}
