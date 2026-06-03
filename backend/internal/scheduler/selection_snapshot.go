package scheduler

import (
	"context"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
)

type selectionSnapshot struct {
	loads             map[int]int
	hasLoads          bool
	familyCooldown    map[int]bool
	hasFamilyCooldown bool
	windowCost        map[int]Schedulability
	hasWindowCost     bool
	rpm               map[int]Schedulability
	hasRPM            bool
	session           map[int]Schedulability
	hasSession        bool
}

type familyCooldownInCooldownBatchTracker interface {
	InCooldownBatch(ctx context.Context, accountIDs []int, family string) map[int]bool
}

type batchSchedulabilityTracker interface {
	GetSchedulabilityBatch(ctx context.Context, accounts []*ent.Account) map[int]Schedulability
}

func (s *Scheduler) newSelectionSnapshot(ctx context.Context, candidates []*ent.Account, model string, now time.Time) *selectionSnapshot {
	runtimeCandidates := schedulableBaseCandidates(candidates, now)
	snap := &selectionSnapshot{
		loads:    s.selectionCurrentLoads(ctx, runtimeCandidates),
		hasLoads: true,
	}

	if batch, ok := s.familyCooldown.(familyCooldownInCooldownBatchTracker); ok {
		snap.familyCooldown = make(map[int]bool)
		snap.hasFamilyCooldown = true
		idsByFamily := make(map[string][]int)
		for _, acc := range runtimeCandidates {
			if acc == nil {
				continue
			}
			family := ModelFamily(acc.Platform, model)
			if family == "" {
				continue
			}
			idsByFamily[family] = append(idsByFamily[family], acc.ID)
		}
		for family, ids := range idsByFamily {
			hits := batch.InCooldownBatch(ctx, ids, family)
			for accountID, inCooldown := range hits {
				if inCooldown {
					snap.familyCooldown[accountID] = true
				}
			}
		}
	}

	return snap
}

func (snap *selectionSnapshot) loadSchedulability(ctx context.Context, s *Scheduler, candidates []*ent.Account) {
	if snap == nil {
		return
	}
	if batch, ok := s.windowCost.(batchSchedulabilityTracker); ok {
		snap.windowCost = batch.GetSchedulabilityBatch(ctx, candidates)
		snap.hasWindowCost = true
	}
	if batch, ok := s.rpm.(batchSchedulabilityTracker); ok {
		snap.rpm = batch.GetSchedulabilityBatch(ctx, candidates)
		snap.hasRPM = true
	}
	if batch, ok := s.session.(batchSchedulabilityTracker); ok {
		snap.session = batch.GetSchedulabilityBatch(ctx, candidates)
		snap.hasSession = true
	}
}

func (s *Scheduler) deferredConstraintCandidates(ctx context.Context, candidates []*ent.Account, model string, now time.Time, snapshot *selectionSnapshot) []*ent.Account {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if acc == nil {
			continue
		}
		if SchedulabilityOf(acc, now) == NotSchedulable {
			continue
		}
		if family := ModelFamily(acc.Platform, model); family != "" && s.familyCooldown != nil {
			inCooldown, fromSnapshot := snapshot.inFamilyCooldown(acc.ID)
			if !fromSnapshot {
				_, inCooldown = s.familyCooldown.Until(ctx, acc.ID, family)
			}
			if inCooldown {
				continue
			}
		}
		if s.concurrencySchedulability(ctx, acc, snapshot) == NotSchedulable {
			continue
		}
		out = append(out, acc)
	}
	return out
}

func schedulableBaseCandidates(candidates []*ent.Account, now time.Time) []*ent.Account {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if acc == nil {
			continue
		}
		if SchedulabilityOf(acc, now) == NotSchedulable {
			continue
		}
		out = append(out, acc)
	}
	return out
}

func (s *Scheduler) selectionCurrentLoads(ctx context.Context, candidates []*ent.Account) map[int]int {
	ids := accountIDsFromCandidates(candidates)
	result := make(map[int]int, len(ids))
	if len(ids) == 0 {
		return result
	}
	if s.currentLoad != nil {
		for _, id := range ids {
			result[id] = s.currentLoad(ctx, id)
		}
		return result
	}
	if s.rdb == nil {
		return result
	}
	return loadConcurrencyCounts(ctx, s.rdb, ids)
}

func accountIDsFromCandidates(candidates []*ent.Account) []int {
	ids := make([]int, 0, len(candidates))
	for _, acc := range candidates {
		if acc != nil && acc.ID > 0 {
			ids = append(ids, acc.ID)
		}
	}
	return uniqueAccountIDs(ids)
}

func (snap *selectionSnapshot) currentLoad(s *Scheduler, ctx context.Context, accountID int) int {
	if snap != nil && snap.hasLoads {
		return snap.loads[accountID]
	}
	return s.getCurrentLoad(ctx, accountID)
}

func (snap *selectionSnapshot) inFamilyCooldown(accountID int) (bool, bool) {
	if snap == nil || !snap.hasFamilyCooldown {
		return false, false
	}
	return snap.familyCooldown[accountID], true
}

func (snap *selectionSnapshot) windowCostSchedulability(accountID int) (Schedulability, bool) {
	if snap == nil || !snap.hasWindowCost {
		return Normal, false
	}
	return snap.windowCost[accountID], true
}

func (snap *selectionSnapshot) rpmSchedulability(accountID int) (Schedulability, bool) {
	if snap == nil || !snap.hasRPM {
		return Normal, false
	}
	return snap.rpm[accountID], true
}

func (snap *selectionSnapshot) sessionSchedulability(accountID int) (Schedulability, bool) {
	if snap == nil || !snap.hasSession {
		return Normal, false
	}
	return snap.session[accountID], true
}
