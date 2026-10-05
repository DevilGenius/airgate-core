package store

import (
	"testing"

	"github.com/DevilGenius/airgate-core/internal/app/account"
)

func TestAccountSchedulingWeightPersistence(t *testing.T) {
	db := enttestOpen(t)
	t.Cleanup(func() { _ = db.Close() })
	store := NewAccountStore(db)
	created, err := store.Create(t.Context(), account.CreateInput{Name: "weighted", Platform: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	if created.SchedulingWeight != 100 {
		t.Fatalf("default = %d", created.SchedulingWeight)
	}
	weight := 0
	updated, err := store.Update(t.Context(), created.ID, account.UpdateInput{SchedulingWeight: &weight})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SchedulingWeight != weight {
		t.Fatalf("updated = %d", updated.SchedulingWeight)
	}
	loaded, err := db.Account.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchedulingWeight != weight {
		t.Fatalf("persisted = %d", loaded.SchedulingWeight)
	}
	for _, invalid := range []int{-1, 1000001} {
		if _, err := store.Update(t.Context(), created.ID, account.UpdateInput{SchedulingWeight: &invalid}); err == nil {
			t.Fatalf("accepted invalid weight %d", invalid)
		}
	}
}
