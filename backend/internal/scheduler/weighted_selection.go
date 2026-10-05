package scheduler

import (
	"context"
	"math/bits"
	"math/rand/v2"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/accountweight"
)

// selectionWeightScale preserves sub-unit capacity without floating point.
const selectionWeightScale uint64 = 1 << 32

func accountSelectionWeight(acc *ent.Account, load int) uint64 {
	if acc.SchedulingWeight == 0 || !accountweight.Valid(acc.SchedulingWeight) {
		return 0
	}
	capacity := acc.MaxConcurrency
	if capacity <= 0 {
		capacity = DefaultAccountMaxConcurrency
	}
	if load >= capacity {
		return 0
	}
	if load < 0 {
		load = 0
	}
	// The intermediate product uses 128 bits, including for MaxInt capacity.
	hi, lo := bits.Mul64(uint64(acc.SchedulingWeight)*selectionWeightScale, uint64(capacity-load))
	weight, _ := bits.Div64(hi, lo, uint64(capacity))
	return max(1, weight)
}

// weightTotal holds the sum of every candidate, without limiting pool size.
// A slice has at most MaxInt entries and each weight is below 2^52, so the
// entire sum fits in 128 bits even on a 64-bit host.
type weightTotal struct{ hi, lo uint64 }

func (total *weightTotal) add(weight uint64) {
	var carry uint64
	total.lo, carry = bits.Add64(total.lo, weight, 0)
	total.hi += carry
}

func (total weightTotal) draw() weightTotal {
	if total.hi == 0 {
		return weightTotal{lo: rand.Uint64N(total.lo)}
	}
	for {
		value := weightTotal{hi: rand.Uint64N(total.hi + 1), lo: rand.Uint64()}
		if value.hi < total.hi || (value.hi == total.hi && value.lo < total.lo) {
			return value
		}
	}
}

// selectByWeight performs weighted reservoir sampling within the highest
// priority layer. Each load is read once; memory is constant and no candidate
// is discarded based on rank, last-used time or pool size. Eligibility and
// affinity are resolved by the caller before reaching this selection step.
func (s *Scheduler) selectByWeight(ctx context.Context, candidates []*ent.Account, snapshot *selectionSnapshot) *ent.Account {
	var selected *ent.Account
	var total weightTotal
	var priority int
	for _, acc := range candidates {
		if acc == nil {
			continue
		}
		if selected != nil && acc.Priority < priority {
			continue
		}
		weight := accountSelectionWeight(acc, snapshot.currentLoad(s, ctx, acc.ID))
		if weight == 0 {
			continue
		}
		if selected == nil || acc.Priority > priority {
			selected, priority = acc, acc.Priority
			total = weightTotal{lo: weight}
			continue
		}
		total.add(weight)
		if draw := total.draw(); draw.hi == 0 && draw.lo < weight {
			selected = acc
		}
	}
	return selected
}
