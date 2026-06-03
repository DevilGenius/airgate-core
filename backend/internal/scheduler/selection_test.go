package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
)

func TestExcludeAccountsDoesNotMutateCandidates(t *testing.T) {
	t.Parallel()

	candidates := []*ent.Account{{ID: 1}, {ID: 2}, {ID: 3}}
	got := excludeAccounts(candidates, []int{2})

	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("excludeAccounts result = %+v, want IDs [1 3]", got)
	}
	if len(candidates) != 3 || candidates[0].ID != 1 || candidates[1].ID != 2 || candidates[2].ID != 3 {
		t.Fatalf("candidates mutated to %+v, want original IDs [1 2 3]", candidates)
	}
}

func TestSelectSoftStickyAccountHonorsHighestPriority(t *testing.T) {
	t.Parallel()

	candidates := []*ent.Account{
		{ID: 1, Priority: 10},
		{ID: 2, Priority: 20},
		{ID: 3, Priority: 20},
	}

	if got := selectSoftStickyAccount(candidates, 1); got != nil {
		t.Fatalf("low priority sticky account selected: %+v", got)
	}
	if got := selectSoftStickyAccount(candidates, 2); got == nil || got.ID != 2 {
		t.Fatalf("top priority sticky account = %+v, want account 2", got)
	}
}

func TestSoftStickyPrefersNormalPriorityPool(t *testing.T) {
	t.Parallel()

	normalCandidates := []*ent.Account{{ID: 2, Priority: 20}}
	stickyCandidates := []*ent.Account{
		{ID: 1, Priority: 30},
		{ID: 2, Priority: 20},
	}

	pool := softStickyCandidates(normalCandidates, stickyCandidates)
	if got := selectSoftStickyAccount(pool, 1); got != nil {
		t.Fatalf("sticky-only account selected while normal candidates exist: %+v", got)
	}
	if got := selectSoftStickyAccount(pool, 2); got == nil || got.ID != 2 {
		t.Fatalf("normal top priority sticky account = %+v, want account 2", got)
	}
}

func TestContinuationBlockedErrorDistinguishesCapacityFromMissingAffinity(t *testing.T) {
	t.Parallel()

	candidates := []*ent.Account{{ID: 1}}
	if err := continuationBlockedError(candidates, 1); !errors.Is(err, ErrContinuationCapacityExceeded) {
		t.Fatalf("continuationBlockedError(existing) = %v, want ErrContinuationCapacityExceeded", err)
	}
	if err := continuationBlockedError(candidates, 2); !errors.Is(err, ErrContinuationAffinityMissing) {
		t.Fatalf("continuationBlockedError(missing) = %v, want ErrContinuationAffinityMissing", err)
	}
}

func TestHardAffinityAllowsWindowCostOverflow(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(NotSchedulable)
	now := time.Now()
	acc := newSelectionTestAccount(10)

	if got := s.checkSchedulability(ctx, acc, "gpt-4.1", now); got != NotSchedulable {
		t.Fatalf("checkSchedulability() = %v, want NotSchedulable", got)
	}
	if got := s.checkHardAffinitySchedulability(ctx, acc, "gpt-4.1", now); got != StickyOnly {
		t.Fatalf("checkHardAffinitySchedulability() = %v, want StickyOnly", got)
	}
}

func TestSelectAccountHardPreviousResponseAllowsWindowCostOverflow(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(NotSchedulable)
	groupID := 7
	acc := newSelectionTestAccount(10)
	s.routeCache.Set(groupID, "openai", []*ent.Account{acc}, nil)

	s.BindResponseAccount(ctx, groupID, "openai", "resp_1", acc.ID)
	if _, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID: "resp_1",
	}); !errors.Is(err, ErrNoAvailableAccount) {
		t.Fatalf("soft previous response err = %v, want ErrNoAvailableAccount", err)
	}

	windowCost := s.windowCost.(*stubWindowCostTracker)
	windowCost.calls = 0
	selected, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID:          "resp_1",
		RequireContinuationAffinity: true,
	})
	if err != nil {
		t.Fatalf("hard previous response returned error: %v", err)
	}
	if selected.ID != acc.ID {
		t.Fatalf("selected account ID = %d, want %d", selected.ID, acc.ID)
	}
	if windowCost.calls != 1 {
		t.Fatalf("window cost checks = %d, want 1", windowCost.calls)
	}
}

func TestHardAffinityDoesNotBypassNonWindowConstraints(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		configure func(s *Scheduler, acc *ent.Account)
	}{
		{
			name: "rpm limit",
			configure: func(s *Scheduler, _ *ent.Account) {
				s.rpm.(*stubRPMTracker).sched = NotSchedulable
			},
		},
		{
			name: "session limit",
			configure: func(s *Scheduler, _ *ent.Account) {
				s.session.(*stubSessionTracker).sched = NotSchedulable
			},
		},
		{
			name: "concurrency limit",
			configure: func(s *Scheduler, acc *ent.Account) {
				acc.MaxConcurrency = 1
				s.currentLoad = func(context.Context, int) int { return 1 }
			},
		},
		{
			name: "disabled account",
			configure: func(_ *Scheduler, acc *ent.Account) {
				acc.State = account.StateDisabled
			},
		},
		{
			name: "family cooldown",
			configure: func(s *Scheduler, _ *ent.Account) {
				s.familyCooldown = stubFamilyCooldownTracker{inCooldown: true}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			s := newSelectionTestScheduler(NotSchedulable)
			groupID := 7
			acc := newSelectionTestAccount(10)
			tt.configure(s, acc)
			s.routeCache.Set(groupID, "openai", []*ent.Account{acc}, nil)
			s.BindResponseAccount(ctx, groupID, "openai", "resp_blocked", acc.ID)

			_, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
				PreviousResponseID:          "resp_blocked",
				RequireContinuationAffinity: true,
			})
			if !errors.Is(err, ErrContinuationCapacityExceeded) {
				t.Fatalf("SelectAccountWithOptions() error = %v, want ErrContinuationCapacityExceeded", err)
			}
		})
	}
}

type stubWindowCostTracker struct {
	sched Schedulability
	calls int
}

func (s *stubWindowCostTracker) GetSchedulability(context.Context, int, map[string]interface{}) Schedulability {
	s.calls++
	return s.sched
}

func (s *stubWindowCostTracker) AddCost(context.Context, int, float64) {}

type stubRPMTracker struct {
	sched Schedulability
}

func (s *stubRPMTracker) IncrementRPM(context.Context, int) (int, error) {
	return 0, nil
}

func (s *stubRPMTracker) TryIncrementRPM(context.Context, int, int) (bool, error) {
	return true, nil
}

func (s *stubRPMTracker) DecrementRPM(context.Context, int) {}

func (s *stubRPMTracker) GetSchedulability(context.Context, int, int) Schedulability {
	return s.sched
}

type stubSessionTracker struct {
	sched Schedulability
}

func (s *stubSessionTracker) RefreshSession(context.Context, int, string, time.Duration) error {
	return nil
}

func (s *stubSessionTracker) RegisterSession(context.Context, int, string, int, time.Duration) (bool, error) {
	return true, nil
}

func (s *stubSessionTracker) GetSchedulability(context.Context, int, map[string]interface{}) Schedulability {
	return s.sched
}

type stubFamilyCooldownTracker struct {
	inCooldown bool
}

func (s stubFamilyCooldownTracker) Until(context.Context, int, string) (time.Time, bool) {
	if !s.inCooldown {
		return time.Time{}, false
	}
	return time.Now().Add(time.Minute), true
}

func (s stubFamilyCooldownTracker) List(context.Context, int) []FamilyCooldownEntry {
	return nil
}

func (s stubFamilyCooldownTracker) ListBatch(context.Context, []int) map[int][]FamilyCooldownEntry {
	return nil
}

func (s stubFamilyCooldownTracker) ClearAccount(context.Context, int) int {
	return 0
}

func newSelectionTestScheduler(windowCostSched Schedulability) *Scheduler {
	return &Scheduler{
		sticky:           NewStickySession(nil),
		windowCost:       &stubWindowCostTracker{sched: windowCostSched},
		rpm:              &stubRPMTracker{sched: Normal},
		session:          &stubSessionTracker{sched: Normal},
		routeCache:       newRouteCache(time.Minute),
		responseAffinity: NewResponseAffinity(nil),
	}
}

func newSelectionTestAccount(id int) *ent.Account {
	return &ent.Account{
		ID:             id,
		Name:           "selection test",
		Platform:       "openai",
		State:          account.StateActive,
		MaxConcurrency: DefaultAccountMaxConcurrency,
		Extra:          map[string]interface{}{},
	}
}

func TestNormalizeGroupLookupErrorPreservesCancellation(t *testing.T) {
	t.Parallel()

	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		got := normalizeGroupLookupError(err)
		if !errors.Is(got, err) {
			t.Fatalf("normalizeGroupLookupError(%v) = %v, want original error", err, got)
		}
	}
}

func TestNormalizeGroupLookupErrorWrapsGenericError(t *testing.T) {
	t.Parallel()

	orig := errors.New("db offline")
	got := normalizeGroupLookupError(orig)
	if errors.Is(got, ErrGroupNotFound) {
		t.Fatalf("normalizeGroupLookupError(%v) = %v, want generic query error", orig, got)
	}
	if got.Error() != "查询分组失败: db offline" {
		t.Fatalf("normalizeGroupLookupError(%v) = %q, want %q", orig, got.Error(), "查询分组失败: db offline")
	}
}

func TestNormalizeGroupAccountsLookupErrorPreservesCancellation(t *testing.T) {
	t.Parallel()

	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		got := normalizeGroupAccountsLookupError(err)
		if !errors.Is(got, err) {
			t.Fatalf("normalizeGroupAccountsLookupError(%v) = %v, want original error", err, got)
		}
	}
}

func TestNormalizeGroupAccountsLookupErrorWrapsGenericError(t *testing.T) {
	t.Parallel()

	orig := errors.New("db offline")
	got := normalizeGroupAccountsLookupError(orig)
	if got.Error() != "查询分组账户失败: db offline" {
		t.Fatalf("normalizeGroupAccountsLookupError(%v) = %q, want %q", orig, got.Error(), "查询分组账户失败: db offline")
	}
}
