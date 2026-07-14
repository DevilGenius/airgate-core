package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"

	"github.com/DevilGenius/airgate-core/ent"
)

type batchFamilyCooldownTracker struct {
	hits  map[string]map[int]bool
	calls map[string][]int
}

func (b *batchFamilyCooldownTracker) Until(context.Context, int, string) (time.Time, bool) {
	return time.Time{}, false
}

func (b *batchFamilyCooldownTracker) List(context.Context, int) []FamilyCooldownEntry {
	return nil
}

func (b *batchFamilyCooldownTracker) ListBatch(context.Context, []int) map[int][]FamilyCooldownEntry {
	return nil
}

func (b *batchFamilyCooldownTracker) ClearAccount(context.Context, int) int {
	return 0
}

func (b *batchFamilyCooldownTracker) InCooldownBatch(_ context.Context, accountIDs []int, family string) map[int]bool {
	if b.calls == nil {
		b.calls = make(map[string][]int)
	}
	b.calls[family] = append([]int(nil), accountIDs...)
	return b.hits[family]
}

type batchSchedTracker struct {
	result map[int]Schedulability
	calls  int
}

func (b *batchSchedTracker) GetSchedulability(context.Context, int, map[string]interface{}) Schedulability {
	return Normal
}

func (b *batchSchedTracker) AddCost(context.Context, int, float64) {}

func (b *batchSchedTracker) IncrementRPM(context.Context, int) (int, error) { return 0, nil }

func (b *batchSchedTracker) TryIncrementRPM(context.Context, int, int) (bool, error) {
	return true, nil
}

func (b *batchSchedTracker) DecrementRPM(context.Context, int) {}

func (b *batchSchedTracker) RefreshSession(context.Context, int, string, time.Duration) error {
	return nil
}

func (b *batchSchedTracker) RegisterSession(context.Context, int, string, int, time.Duration) (bool, error) {
	return true, nil
}

func (b *batchSchedTracker) GetSchedulabilityBatch(_ context.Context, accounts []*ent.Account) map[int]Schedulability {
	b.calls++
	out := make(map[int]Schedulability)
	for _, acc := range accounts {
		if acc == nil {
			continue
		}
		if sched, ok := b.result[acc.ID]; ok {
			out[acc.ID] = sched
		}
	}
	return out
}

type batchRPMSchedTracker struct {
	result map[int]Schedulability
	calls  int
}

func (b *batchRPMSchedTracker) IncrementRPM(context.Context, int) (int, error) { return 0, nil }

func (b *batchRPMSchedTracker) TryIncrementRPM(context.Context, int, int) (bool, error) {
	return true, nil
}

func (b *batchRPMSchedTracker) DecrementRPM(context.Context, int) {}

func (b *batchRPMSchedTracker) GetSchedulability(context.Context, int, int) Schedulability {
	return Normal
}

func (b *batchRPMSchedTracker) GetSchedulabilityBatch(_ context.Context, accounts []*ent.Account) map[int]Schedulability {
	b.calls++
	out := make(map[int]Schedulability)
	for _, acc := range accounts {
		if acc == nil {
			continue
		}
		if sched, ok := b.result[acc.ID]; ok {
			out[acc.ID] = sched
		}
	}
	return out
}

func TestSelectionSnapshotRedisLoadAndFamilyCooldown(t *testing.T) {
	ctx := context.Background()
	rdb, mock := redismock.NewClientMock()
	s := &Scheduler{rdb: rdb, familyCooldown: NewFamilyCooldown(rdb)}
	candidates := []*ent.Account{
		{ID: 1, Platform: "openai"},
		nil,
		{ID: 0, Platform: "openai"},
		{ID: 2, Platform: "openai"},
	}
	family := ModelFamily("openai", "gpt-4.1")
	mock.ExpectMGet(
		concurrencyCountKey(1),
		concurrencyCountKey(2),
		familyCooldownReasonKey(1, family),
		familyCooldownReasonKey(2, family),
	).SetVal([]interface{}{"3", nil, "rate limited", nil})

	snap := &selectionSnapshot{loads: map[int]int{}}
	loadedFamily := s.loadRedisSelectionSnapshot(ctx, candidates, "gpt-4.1", snap)
	if !loadedFamily || !snap.hasFamilyCooldown || snap.loads[1] != 3 || snap.loads[2] != 0 || !snap.familyCooldown[1] || snap.familyCooldown[2] {
		t.Fatalf("redis snapshot loaded=%v snap=%+v", loadedFamily, snap)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	s = &Scheduler{rdb: rdb, familyCooldown: NewFamilyCooldown(rdb)}
	mock.ExpectMGet(concurrencyCountKey(1), familyCooldownReasonKey(1, family)).SetErr(errors.New("redis down"))
	snap = &selectionSnapshot{loads: map[int]int{}}
	if loadedFamily = s.loadRedisSelectionSnapshot(ctx, []*ent.Account{{ID: 1, Platform: "openai"}}, "gpt-4.1", snap); !loadedFamily {
		t.Fatal("redis snapshot should report familyLoaded even when MGET fails")
	}
	if snap.loads[1] != 0 || snap.familyCooldown[1] {
		t.Fatalf("failed redis snapshot should fail open: %+v", snap)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis error expectations: %v", err)
	}

	snap = &selectionSnapshot{loads: map[int]int{}}
	if loadedFamily = (&Scheduler{}).loadRedisSelectionSnapshot(ctx, nil, "gpt-4.1", snap); loadedFamily {
		t.Fatal("empty redis snapshot without FamilyCooldown should not load family")
	}
}

func TestSelectionSnapshotFamilyAndSchedulabilityBatch(t *testing.T) {
	ctx := context.Background()
	familyTracker := &batchFamilyCooldownTracker{hits: map[string]map[int]bool{
		ModelFamily("openai", "gpt-4.1"): {1: true, 2: false, 3: true},
	}}
	s := &Scheduler{familyCooldown: familyTracker}
	candidates := []*ent.Account{
		{ID: 1, Platform: "openai"},
		{ID: 2, Platform: "openai"},
		{ID: 3, Platform: "claude"},
		nil,
		{ID: 4},
	}
	snap := &selectionSnapshot{}
	s.loadFamilyCooldownSnapshot(ctx, candidates, "gpt-4.1", snap)
	if !snap.hasFamilyCooldown || !snap.familyCooldown[1] || snap.familyCooldown[2] || !snap.familyCooldown[3] {
		t.Fatalf("family cooldown snapshot = %+v", snap.familyCooldown)
	}
	if len(familyTracker.calls[ModelFamily("openai", "gpt-4.1")]) != 4 {
		t.Fatalf("family batch calls = %+v", familyTracker.calls)
	}

	window := &batchSchedTracker{result: map[int]Schedulability{1: StickyOnly}}
	rpm := &batchRPMSchedTracker{result: map[int]Schedulability{2: NotSchedulable}}
	session := &batchSchedTracker{result: map[int]Schedulability{3: StickyOnly}}
	s = &Scheduler{windowCost: window, rpm: rpm, session: session}
	snap.loadSchedulability(ctx, s, candidates)
	if !snap.hasWindowCost || !snap.hasRPM || !snap.hasSession || window.calls != 1 || rpm.calls != 1 || session.calls != 1 {
		t.Fatalf("sched snapshot flags window=%v rpm=%v session=%v calls=%d/%d/%d", snap.hasWindowCost, snap.hasRPM, snap.hasSession, window.calls, rpm.calls, session.calls)
	}
	if got, ok := snap.windowCostSchedulability(1); !ok || got != StickyOnly {
		t.Fatalf("window sched = %v/%v", got, ok)
	}
	if got, ok := snap.rpmSchedulability(2); !ok || got != NotSchedulable {
		t.Fatalf("rpm sched = %v/%v", got, ok)
	}
	if got, ok := snap.sessionSchedulability(3); !ok || got != StickyOnly {
		t.Fatalf("session sched = %v/%v", got, ok)
	}
	if got, ok := snap.windowCostSchedulability(999); ok || got != Normal {
		t.Fatalf("missing window sched = %v/%v", got, ok)
	}
	if got, ok := ((*selectionSnapshot)(nil)).rpmSchedulability(1); ok || got != Normal {
		t.Fatalf("nil rpm sched = %v/%v", got, ok)
	}
	if got, ok := (&selectionSnapshot{}).sessionSchedulability(1); ok || got != Normal {
		t.Fatalf("empty session sched = %v/%v", got, ok)
	}
}

func TestSelectionSnapshotCurrentLoadsAndCandidates(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	candidates := []*ent.Account{
		nil,
		{ID: 1, State: "active"},
		{ID: 2, State: "disabled"},
		{ID: 3, State: "degraded", StateUntil: &future, Extra: map[string]interface{}{transientAvoidStepExtraKey: 2}},
		{ID: 4, State: "degraded", StateUntil: &past},
	}
	runtime := runtimeConstraintCandidates(candidates, now)
	if len(runtime) != 2 || runtime[0].ID != 1 || runtime[1].ID != 4 {
		t.Fatalf("runtime candidates = %+v", accountIDsFromCandidates(runtime))
	}
	if ids := accountIDsFromCandidates([]*ent.Account{{ID: 3}, nil, {ID: 3}, {ID: 0}, {ID: 2}}); len(ids) != 2 || ids[0] != 3 || ids[1] != 2 {
		t.Fatalf("accountIDsFromCandidates = %v", ids)
	}

	s := &Scheduler{currentLoad: func(_ context.Context, id int) int { return id * 10 }}
	loads := s.selectionCurrentLoads(ctx, []*ent.Account{{ID: 2}, {ID: 3}})
	if loads[2] != 20 || loads[3] != 30 {
		t.Fatalf("current loads = %+v", loads)
	}
	if got := (&selectionSnapshot{hasLoads: true, loads: map[int]int{9: 99}}).currentLoad(s, ctx, 9); got != 99 {
		t.Fatalf("snapshot current load = %d", got)
	}
}
