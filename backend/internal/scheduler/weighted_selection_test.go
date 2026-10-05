package scheduler

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/accountweight"
)

func TestAccountSelectionWeightBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		weight, capacity, load int
		want                   uint64
	}{
		{"idle", 100, 10, 0, 100 * selectionWeightScale},
		{"half", 100, 10, 5, 50 * selectionWeightScale},
		{"weighted half", 200, 10, 5, 100 * selectionWeightScale},
		{"full", 100, 10, 10, 0},
		{"over capacity", 100, 10, 11, 0},
		{"default capacity", 100, 0, DefaultAccountMaxConcurrency / 2, 50 * selectionWeightScale},
		{"invalid capacity", 100, -1, 0, 100 * selectionWeightScale},
		{"negative load", 100, 10, -1, 100 * selectionWeightScale},
		{"zero weight", 0, 10, 0, 0},
		{"negative weight", -1, 10, 0, 0},
		{"excessive weight", accountweight.Max + 1, 10, 0, 0},
		{"wide product", accountweight.Max, math.MaxInt, 0, uint64(accountweight.Max) * selectionWeightScale},
		{"positive minimum", 1, math.MaxInt, math.MaxInt - 1, max(1, selectionWeightScale/uint64(math.MaxInt))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acc := &ent.Account{SchedulingWeight: tc.weight, MaxConcurrency: tc.capacity}
			if got := accountSelectionWeight(acc, tc.load); got != tc.want {
				t.Fatalf("weight = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWeightedSelectionDistribution(t *testing.T) {
	for _, tc := range []struct {
		name           string
		weights, loads []int
		probabilities  []float64
	}{
		{"equal", []int{100, 100}, []int{0, 0}, []float64{0.5, 0.5}},
		{"configuration", []int{100, 200}, []int{0, 0}, []float64{1.0 / 3, 2.0 / 3}},
		{"load", []int{100, 100}, []int{0, 5}, []float64{2.0 / 3, 1.0 / 3}},
		{"combined", []int{100, 100, 200}, []int{0, 5, 5}, []float64{0.4, 0.2, 0.4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := make([]*ent.Account, len(tc.weights))
			snap := &selectionSnapshot{hasLoads: true, loads: map[int]int{}}
			for i, weight := range tc.weights {
				pool[i] = &ent.Account{ID: i + 1, SchedulingWeight: weight, MaxConcurrency: 10}
				snap.loads[i+1] = tc.loads[i]
			}
			counts := make([]int, len(pool))
			s := &Scheduler{}
			const samples = 60000
			for range samples {
				counts[s.selectByWeight(t.Context(), pool, snap).ID-1]++
			}
			for i, want := range tc.probabilities {
				if got := float64(counts[i]) / samples; math.Abs(got-want) > 0.015 {
					t.Fatalf("account %d probability = %.4f, want %.4f", i+1, got, want)
				}
			}
		})
	}
}

func TestWeightedSelectionIncludesWholePoolAndIgnoresLastUsed(t *testing.T) {
	pool := make([]*ent.Account, 64)
	for i := range pool {
		used := time.Now().Add(-time.Duration(i) * time.Hour)
		pool[i] = &ent.Account{ID: i + 1, SchedulingWeight: 100, MaxConcurrency: 10, LastUsedAt: &used}
	}
	reads := 0
	s := &Scheduler{currentLoad: func(context.Context, int) int { reads++; return 0 }}
	counts := make([]int, len(pool))
	const samples = 64000
	for range samples {
		counts[s.selectByWeight(t.Context(), pool, nil).ID-1]++
	}
	if reads != samples*len(pool) {
		t.Fatalf("load reads = %d, want %d", reads, samples*len(pool))
	}
	for id, count := range counts {
		if math.Abs(float64(count)/samples-1.0/64) > 0.004 {
			t.Fatalf("account %d received %d selections", id+1, count)
		}
	}
}

func TestWeightedSelectionRejectsFullSingleAccountAndEmptyPool(t *testing.T) {
	s := &Scheduler{}
	acc := &ent.Account{ID: 1, MaxConcurrency: 1, SchedulingWeight: 100}
	snap := &selectionSnapshot{hasLoads: true, loads: map[int]int{1: 1}}
	for _, pool := range [][]*ent.Account{nil, {nil}, {acc}} {
		if got := s.selectByWeight(t.Context(), pool, snap); got != nil {
			t.Fatalf("unexpected selection: %+v", got)
		}
	}
}

func TestWeightTotalExceedsUint64(t *testing.T) {
	total := weightTotal{lo: math.MaxUint64}
	total.add(10)
	if total.hi != 1 || total.lo != 9 {
		t.Fatalf("sum = %+v", total)
	}
	for range 1000 {
		got := total.draw()
		if got.hi > total.hi || (got.hi == total.hi && got.lo >= total.lo) {
			t.Fatalf("draw %+v is outside %+v", got, total)
		}
	}
}

func TestWeightChangePreservesSoftAndHardAffinity(t *testing.T) {
	for _, hard := range []bool{false, true} {
		t.Run(fmt.Sprint(hard), func(t *testing.T) {
			s := newSelectionTestScheduler(Normal)
			bound, other := newSelectionTestAccount(1), newSelectionTestAccount(2)
			bound.SchedulingWeight, other.SchedulingWeight = 1, accountweight.Max
			seedSelectionTestGroup(t, 7, "openai", []*ent.Account{bound, other}, nil)
			s.sticky.Set(t.Context(), 1, "openai", "session", bound.ID)
			got, err := s.SelectAccountWithOptions(t.Context(), "openai", "gpt-5", 1, 7, "session", AccountSelectionOptions{RequireContinuationAffinity: hard})
			if err != nil || got.ID != bound.ID {
				t.Fatalf("selection = %+v, err = %v", got, err)
			}
		})
	}
}

func BenchmarkWeightedSelectionLargePool(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			pool := make([]*ent.Account, size)
			for i := range pool {
				pool[i] = &ent.Account{ID: i + 1, SchedulingWeight: 100, MaxConcurrency: 10}
			}
			s := &Scheduler{}
			snap := &selectionSnapshot{hasLoads: true}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.selectByWeight(b.Context(), pool, snap)
			}
		})
	}
}
