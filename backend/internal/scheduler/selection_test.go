package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
)

type captureMonitorRecorder struct {
	events []monitoring.EventInput
}

func (r *captureMonitorRecorder) Record(_ context.Context, input monitoring.EventInput) {
	r.events = append(r.events, input)
}

func (r *captureMonitorRecorder) ResolveBySubject(context.Context, monitoring.ResolveQuery) {}

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

func TestSoftStickyKeepsNegativeFallbackBehindNonNegativeStickyOnly(t *testing.T) {
	t.Parallel()

	normalCandidates := []*ent.Account{{ID: 1, Priority: -1}}
	stickyCandidates := []*ent.Account{
		{ID: 1, Priority: -1},
		{ID: 2, Priority: 0},
	}

	pool := softStickyCandidates(normalCandidates, stickyCandidates)
	if got := selectSoftStickyAccount(pool, 1); got != nil {
		t.Fatalf("negative fallback sticky account selected while non-negative StickyOnly exists: %+v", got)
	}
	if got := selectSoftStickyAccount(pool, 2); got == nil || got.ID != 2 {
		t.Fatalf("non-negative StickyOnly sticky account = %+v, want account 2", got)
	}
}

func TestRecordNoAvailableAccountSkipsFailoverExhaustion(t *testing.T) {
	t.Parallel()

	recorder := &captureMonitorRecorder{}
	s := &Scheduler{state: &StateMachine{monitor: recorder}}

	s.recordNoAvailableAccount(context.Background(), "openai", "gpt-4.1", 2, 7, "", AccountSelectionOptions{}, []int{101})

	if len(recorder.events) != 0 {
		t.Fatalf("events = %d, want 0", len(recorder.events))
	}
}

func TestRecordNoAvailableAccountRecordsInitialExhaustion(t *testing.T) {
	t.Parallel()

	recorder := &captureMonitorRecorder{}
	s := &Scheduler{state: &StateMachine{monitor: recorder}}

	s.recordNoAvailableAccount(context.Background(), "openai", "gpt-4.1", 2, 7, "", AccountSelectionOptions{
		GroupNameSnapshot: "production",
	}, nil)

	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.ErrorCode != "no_available_account" {
		t.Fatalf("errorCode = %q, want no_available_account", event.ErrorCode)
	}
	if event.SubjectID != "7" {
		t.Fatalf("subjectID = %q, want 7", event.SubjectID)
	}
	if got := event.Detail["exclude_count"]; got != 0 {
		t.Fatalf("exclude_count = %#v, want 0", got)
	}
	if got := event.Detail["group_name"]; got != "production" {
		t.Fatalf("group_name = %#v, want production", got)
	}
}

func TestSelectByLoadBalanceUsesNegativePriorityAsFallback(t *testing.T) {
	t.Parallel()

	s := newSelectionTestScheduler(Normal)
	now := time.Now()

	selected := s.selectByLoadBalance(context.Background(), []*ent.Account{
		{ID: 1, Priority: -1},
		{ID: 2, Priority: 0},
		{ID: 3, Priority: -2},
	}, now, nil)
	if selected == nil || selected.ID != 2 {
		t.Fatalf("selected account = %+v, want priority 0 account", selected)
	}

	selected = s.selectByLoadBalance(context.Background(), []*ent.Account{
		{ID: 1, Priority: -2},
		{ID: 2, Priority: -1},
	}, now, nil)
	if selected == nil || selected.ID != 2 {
		t.Fatalf("selected account = %+v, want priority -1 account", selected)
	}
}

func TestSelectByLoadBalanceScoresSamePriorityTier(t *testing.T) {
	t.Parallel()

	s := newSelectionTestScheduler(Normal)
	now := time.Now()
	s.currentLoad = func(_ context.Context, accountID int) int {
		if accountID%3 == 0 {
			return 2
		}
		return 0
	}

	candidates := make([]*ent.Account, 0, 40)
	for i := 1; i <= 40; i++ {
		acc := newSelectionTestAccount(i)
		acc.Priority = 5
		acc.MaxConcurrency = 4
		if i%2 == 0 {
			used := now.Add(-time.Duration(i) * time.Minute)
			acc.LastUsedAt = &used
		}
		candidates = append(candidates, acc)
	}
	selected := s.selectByLoadBalance(context.Background(), candidates, now, nil)
	if selected == nil || selected.Priority != 5 {
		t.Fatalf("selected account = %+v, want same priority candidate", selected)
	}
}

func TestSelectAccountKeepsNegativeFallbackBehindNonNegativeStickyOnly(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(Normal)
	groupID := 7
	primary := newSelectionTestAccount(10)
	primary.Priority = 0
	primary.MaxConcurrency = 10
	fallback := newSelectionTestAccount(20)
	fallback.Priority = -1
	fallback.MaxConcurrency = 10
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{fallback, primary}, nil)

	s.currentLoad = func(_ context.Context, accountID int) int {
		if accountID == primary.ID {
			return 8
		}
		return 0
	}
	selected, err := s.SelectAccount(ctx, "openai", "gpt-4.1", 1, groupID, "")
	if err != nil {
		t.Fatalf("SelectAccount() with StickyOnly primary returned error: %v", err)
	}
	if selected.ID != primary.ID {
		t.Fatalf("selected account ID = %d, want StickyOnly non-negative account %d", selected.ID, primary.ID)
	}

	s.currentLoad = func(_ context.Context, accountID int) int {
		if accountID == primary.ID {
			return 10
		}
		return 0
	}
	selected, err = s.SelectAccount(ctx, "openai", "gpt-4.1", 1, groupID, "")
	if err != nil {
		t.Fatalf("SelectAccount() with full primary returned error: %v", err)
	}
	if selected.ID != fallback.ID {
		t.Fatalf("selected account ID = %d, want negative fallback account %d", selected.ID, fallback.ID)
	}
}

func TestSelectAccountRoutesNegativePriorityOnlyAfterNonNegativeUnavailable(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(Normal)
	groupID := 7
	primary := newSelectionTestAccount(10)
	primary.Priority = 0
	fallback := newSelectionTestAccount(20)
	fallback.Priority = -1
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{fallback, primary}, nil)

	selected, err := s.SelectAccount(ctx, "openai", "gpt-4.1", 1, groupID, "")
	if err != nil {
		t.Fatalf("SelectAccount() returned error: %v", err)
	}
	if selected.ID != primary.ID {
		t.Fatalf("selected account ID = %d, want non-negative account %d", selected.ID, primary.ID)
	}

	primary.State = account.StateDisabled
	selected, err = s.SelectAccount(ctx, "openai", "gpt-4.1", 1, groupID, "")
	if err != nil {
		t.Fatalf("SelectAccount() with disabled primary returned error: %v", err)
	}
	if selected.ID != fallback.ID {
		t.Fatalf("selected account ID = %d, want negative fallback account %d", selected.ID, fallback.ID)
	}
}

func TestSelectAccountSkipsShortDegradedWindow(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(Normal)
	groupID := 7
	primary := newSelectionTestAccount(10)
	primary.Priority = 20
	until := time.Now().Add(15 * time.Second)
	primary.State = account.StateDegraded
	primary.StateUntil = &until
	primary.Extra = map[string]interface{}{transientAvoidStepExtraKey: 2}
	fallback := newSelectionTestAccount(20)
	fallback.Priority = 10
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{primary, fallback}, nil)

	selected, err := s.SelectAccount(ctx, "openai", "gpt-4.1", 1, groupID, "")
	if err != nil {
		t.Fatalf("SelectAccount() returned error: %v", err)
	}
	if selected.ID != fallback.ID {
		t.Fatalf("selected account ID = %d, want fallback account %d", selected.ID, fallback.ID)
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

func TestSelectionSmallBranchHelpers(t *testing.T) {
	t.Parallel()

	if got := findAccountByID([]*ent.Account{{ID: 1}}, 0); got != nil {
		t.Fatalf("findAccountByID(0) = %+v, want nil", got)
	}
	filtered := filterPriorityCandidates([]*ent.Account{
		nil,
		&ent.Account{ID: 1, Priority: -1},
		&ent.Account{ID: 2, Priority: 0},
	}, true)
	if len(filtered) != 1 || filtered[0].ID != 1 {
		t.Fatalf("filterPriorityCandidates negative = %+v", filtered)
	}
	affinity := &ent.Account{ID: 1, Priority: -2}
	competitor := &ent.Account{ID: 2, Priority: -1}
	if !softAffinityCompetitorBlocks(affinity, StickyOnly, competitor, StickyOnly) {
		t.Fatal("negative sticky affinity should be blocked by higher negative sticky competitor")
	}
	if softAffinityCompetitorBlocks(affinity, Normal, competitor, StickyOnly) {
		t.Fatal("negative normal affinity should not be blocked by sticky-only negative competitor")
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
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{acc}, nil)

	s.BindResponseAccount(ctx, groupID, "openai", "resp_1", acc.ID)
	if _, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID: "resp_1",
	}); !errors.Is(err, ErrPreviousResponseAffinitySkip) {
		t.Fatalf("soft previous response err = %v, want ErrPreviousResponseAffinitySkip", err)
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

func TestSelectAccountHardPreviousResponseAllowsDegradedProbe(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(Normal)
	groupID := 7
	acc := newSelectionTestAccount(10)
	until := time.Now().Add(time.Minute)
	acc.State = account.StateDegraded
	acc.StateUntil = &until
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{acc}, nil)
	s.BindResponseAccount(ctx, groupID, "openai", "resp_probe", acc.ID)

	selected, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID:          "resp_probe",
		RequireContinuationAffinity: true,
	})
	if err != nil {
		t.Fatalf("SelectAccountWithOptions() returned error: %v", err)
	}
	if selected.ID != acc.ID {
		t.Fatalf("selected account ID = %d, want %d", selected.ID, acc.ID)
	}
}

func TestSelectAccountHardPreviousResponseBlocksKnownCooldown(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		configure func(s *Scheduler, acc *ent.Account)
	}{
		{
			name: "account rate limited",
			configure: func(_ *Scheduler, acc *ent.Account) {
				until := time.Now().Add(time.Minute)
				acc.State = account.StateRateLimited
				acc.StateUntil = &until
			},
		},
		{
			name: "family cooldown",
			configure: func(s *Scheduler, _ *ent.Account) {
				s.familyCooldown = stubFamilyCooldownTracker{inCooldown: true}
			},
		},
		{
			name: "short degraded window",
			configure: func(s *Scheduler, acc *ent.Account) {
				until := time.Now().Add(15 * time.Second)
				acc.State = account.StateDegraded
				acc.StateUntil = &until
				acc.Extra = map[string]interface{}{transientAvoidStepExtraKey: 2}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			s := newSelectionTestScheduler(Normal)
			groupID := 7
			acc := newSelectionTestAccount(10)
			tt.configure(s, acc)
			seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{acc}, nil)
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

func TestSoftPreviousResponseAffinityRequiresHighestPriority(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(Normal)
	groupID := 7
	low := newSelectionTestAccount(10)
	low.Priority = 10
	high := newSelectionTestAccount(20)
	high.Priority = 20
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{low, high}, nil)

	s.BindResponseAccount(ctx, groupID, "openai", "resp_low", low.ID)
	_, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID: "resp_low",
	})
	if !errors.Is(err, ErrPreviousResponseAffinitySkip) {
		t.Fatalf("SelectAccountWithOptions(low affinity) error = %v, want ErrPreviousResponseAffinitySkip", err)
	}

	s.BindResponseAccount(ctx, groupID, "openai", "resp_high", high.ID)
	selected, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID: "resp_high",
	})
	if err != nil {
		t.Fatalf("SelectAccountWithOptions(high affinity) returned error: %v", err)
	}
	if selected.ID != high.ID {
		t.Fatalf("selected account ID = %d, want %d", selected.ID, high.ID)
	}
}

func TestSoftPreviousResponseAffinityFastPathSkipsLowerPriorityCapacityChecks(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(Normal)
	groupID := 7
	low := newSelectionTestAccount(10)
	low.Priority = 10
	fallback := newSelectionTestAccount(15)
	fallback.Priority = -1
	high := newSelectionTestAccount(20)
	high.Priority = 20
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{low, fallback, high}, nil)
	s.BindResponseAccount(ctx, groupID, "openai", "resp_high", high.ID)

	windowCost := s.windowCost.(*stubWindowCostTracker)
	windowCost.calls = 0
	selected, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID: "resp_high",
	})
	if err != nil {
		t.Fatalf("SelectAccountWithOptions() returned error: %v", err)
	}
	if selected.ID != high.ID {
		t.Fatalf("selected account ID = %d, want %d", selected.ID, high.ID)
	}
	if windowCost.calls != 1 {
		t.Fatalf("window cost checks = %d, want 1", windowCost.calls)
	}
}

func TestHardPreviousResponseAffinityFastPathSkipsUnrelatedCapacityChecks(t *testing.T) {
	ctx := context.Background()
	s := newSelectionTestScheduler(NotSchedulable)
	groupID := 7
	affinity := newSelectionTestAccount(10)
	other := newSelectionTestAccount(20)
	other.Priority = 100
	seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{other, affinity}, nil)
	s.BindResponseAccount(ctx, groupID, "openai", "resp_affinity", affinity.ID)

	windowCost := s.windowCost.(*stubWindowCostTracker)
	windowCost.calls = 0
	selected, err := s.SelectAccountWithOptions(ctx, "openai", "gpt-4.1", 1, groupID, "", AccountSelectionOptions{
		PreviousResponseID:          "resp_affinity",
		RequireContinuationAffinity: true,
	})
	if err != nil {
		t.Fatalf("SelectAccountWithOptions() returned error: %v", err)
	}
	if selected.ID != affinity.ID {
		t.Fatalf("selected account ID = %d, want %d", selected.ID, affinity.ID)
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
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			s := newSelectionTestScheduler(NotSchedulable)
			groupID := 7
			acc := newSelectionTestAccount(10)
			tt.configure(s, acc)
			seedSelectionTestGroup(t, groupID, "openai", []*ent.Account{acc}, nil)
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

type scriptedSessionTracker struct {
	stubSessionTracker
	allowed []bool
	calls   []int
}

func (s *scriptedSessionTracker) RegisterSession(_ context.Context, accountID int, _ string, _ int, _ time.Duration) (bool, error) {
	s.calls = append(s.calls, accountID)
	if len(s.allowed) == 0 {
		return false, nil
	}
	allowed := s.allowed[0]
	s.allowed = s.allowed[1:]
	return allowed, nil
}

func TestMaybeRegisterSessionRetriesAndReportsExhaustion(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	primary := newSelectionTestAccount(10)
	primary.Extra = map[string]interface{}{"max_sessions": 1}
	fallback := newSelectionTestAccount(20)
	fallback.Extra = map[string]interface{}{"max_sessions": 1}

	session := &scriptedSessionTracker{allowed: []bool{false, true}}
	s := newSelectionTestScheduler(Normal)
	s.session = session
	selected, err := s.maybeRegisterSession(ctx, primary, 7, "openai", "sess", []*ent.Account{primary, fallback}, now, nil)
	if err != nil {
		t.Fatalf("maybeRegisterSession retry error = %v", err)
	}
	if selected.ID != fallback.ID {
		t.Fatalf("selected account = %d, want fallback %d", selected.ID, fallback.ID)
	}
	if len(session.calls) != 2 || session.calls[0] != primary.ID || session.calls[1] != fallback.ID {
		t.Fatalf("RegisterSession calls = %v", session.calls)
	}
	if stickyID, ok := s.sticky.Get(ctx, 7, "openai", "sess"); !ok || stickyID != fallback.ID {
		t.Fatalf("sticky after retry = %d/%v, want fallback", stickyID, ok)
	}

	session = &scriptedSessionTracker{allowed: []bool{false}}
	s = newSelectionTestScheduler(Normal)
	s.session = session
	if _, err := s.maybeRegisterSession(ctx, primary, 7, "openai", "sess", []*ent.Account{primary}, now, nil); !errors.Is(err, ErrNoAvailableAccount) {
		t.Fatalf("maybeRegisterSession exhausted error = %v, want ErrNoAvailableAccount", err)
	}
}

func TestRouteAccountsAndCurrentLoadEdges(t *testing.T) {
	restore := routegraph.SetSnapshotForTesting(nil)
	defer restore()
	s := newSelectionTestScheduler(Normal)
	if _, err := s.routeAccounts("openai", "gpt-4.1", 999); !errors.Is(err, ErrGroupNotFound) {
		t.Fatalf("routeAccounts missing group error = %v", err)
	}

	seedSelectionTestGroup(t, 100, "claude", []*ent.Account{newSelectionTestAccount(1)}, nil)
	if _, err := s.routeAccounts("openai", "gpt-4.1", 100); !errors.Is(err, ErrNoAvailableAccount) {
		t.Fatalf("routeAccounts platform mismatch error = %v", err)
	}

	s.currentLoad = func(context.Context, int) int { return 7 }
	if got := s.getCurrentLoad(context.Background(), 1); got != 7 {
		t.Fatalf("getCurrentLoad callback = %d, want 7", got)
	}
	s.currentLoad = nil
	if got := s.getCurrentLoad(context.Background(), 1); got != 0 {
		t.Fatalf("getCurrentLoad without redis = %d, want 0", got)
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
		responseAffinity: NewResponseAffinity(nil),
	}
}

func seedSelectionTestGroup(t *testing.T, groupID int, platform string, accounts []*ent.Account, routing map[string][]int64) {
	t.Helper()
	group := &ent.Group{
		ID:           groupID,
		Name:         "selection test group",
		Platform:     platform,
		ModelRouting: routing,
	}
	group.Edges.Accounts = accounts
	restore := routegraph.SetSnapshotForTesting([]*ent.Group{group})
	t.Cleanup(restore)
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
