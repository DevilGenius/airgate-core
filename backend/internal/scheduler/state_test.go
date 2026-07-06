package scheduler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/ent/migrate"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestStateMachineTransientAvoidanceBackoffAnd403NeverDisables(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_transient_403_backoff")
	sm := NewStateMachine(db, nil)
	criticalTransitions := 0
	sm.onCriticalTransition = func(int) { criticalTransitions++ }

	acc := createStateMachineAccount(ctx, db, "temporary 403", false)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh := db.Account.GetX(ctx, acc.ID)
	assertShortDBAvoidance(t, fresh, 1, 7*time.Second, 8*time.Second)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh = db.Account.GetX(ctx, acc.ID)
	assertShortDBAvoidance(t, fresh, 2, 14*time.Second, 16*time.Second)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh = db.Account.GetX(ctx, acc.ID)
	assertShortDBAvoidance(t, fresh, 3, 29*time.Second, 31*time.Second)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh = db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after fourth 403 = %s, want degraded", fresh.State)
	}
	assertDBDegraded(t, fresh, 59*time.Second, 61*time.Second)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh = db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after fifth 403 = %s, want degraded", fresh.State)
	}
	assertDBDegraded(t, fresh, 59*time.Second, 61*time.Second)

	applyJudgmentN(ctx, sm, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"}, 4)
	fresh = db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after repeated 403 = %s, want degraded", fresh.State)
	}
	assertDBDegraded(t, fresh, 59*time.Second, 61*time.Second)
	if criticalTransitions != 0 {
		t.Fatalf("critical transitions = %d, want 0", criticalTransitions)
	}
}

func TestStateMachineAccountDead403DegradesWithoutDisable(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_dead_403_degrades")
	sm := NewStateMachine(db, nil)
	criticalTransitions := 0
	sm.onCriticalTransition = func(int) { criticalTransitions++ }
	acc := createStateMachineAccount(ctx, db, "account dead 403", false)

	applyJudgmentN(ctx, sm, acc.ID, Judgment{
		Kind:           sdk.OutcomeAccountDead,
		Reason:         "HTTP 403: forbidden",
		UpstreamStatus: http.StatusForbidden,
	}, 8)

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after repeated account-dead 403 = %s, want degraded", fresh.State)
	}
	assertDBDegraded(t, fresh, 59*time.Second, 61*time.Second)
	if criticalTransitions != 0 {
		t.Fatalf("critical transitions = %d, want 0", criticalTransitions)
	}
}

func TestStateMachineAccountDead403InactiveWorkspaceMemberDisables(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_dead_403_inactive_workspace_member")
	sm := NewStateMachine(db, nil)
	criticalTransitions := 0
	sm.onCriticalTransition = func(int) { criticalTransitions++ }
	acc := createStateMachineAccount(ctx, db, "account dead inactive workspace", false)

	sm.Apply(ctx, acc.ID, Judgment{
		Kind:           sdk.OutcomeAccountDead,
		Reason:         "HTTP 403: Personal access token owner is not an active member of the selected workspace.",
		UpstreamStatus: http.StatusForbidden,
	})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDisabled {
		t.Fatalf("state after inactive workspace member = %s, want disabled", fresh.State)
	}
	if fresh.StateUntil != nil {
		t.Fatalf("state_until after inactive workspace member = %v, want nil", fresh.StateUntil)
	}
	if criticalTransitions != 1 {
		t.Fatalf("critical transitions = %d, want 1", criticalTransitions)
	}
}

func TestStateMachineAccountDead401StillDisables(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_dead_401_disables")
	sm := NewStateMachine(db, nil)
	criticalTransitions := 0
	sm.onCriticalTransition = func(int) { criticalTransitions++ }
	acc := createStateMachineAccount(ctx, db, "account dead 401", false)

	sm.Apply(ctx, acc.ID, Judgment{
		Kind:           sdk.OutcomeAccountDead,
		Reason:         "HTTP 401: invalid token",
		UpstreamStatus: http.StatusUnauthorized,
	})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDisabled {
		t.Fatalf("state after account-dead 401 = %s, want disabled", fresh.State)
	}
	if fresh.StateUntil != nil {
		t.Fatalf("state_until after account-dead 401 = %v, want nil", fresh.StateUntil)
	}
	if criticalTransitions != 1 {
		t.Fatalf("critical transitions = %d, want 1", criticalTransitions)
	}
}

func TestStateMachineAccountMonitorEventUsesSnapshotAndWarning(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_monitor_snapshot")
	recorder := &captureMonitorRecorder{}
	sm := NewStateMachine(db, nil)
	sm.monitor = recorder
	acc := createStateMachineAccount(ctx, db, "openai primary", false)

	sm.Apply(ctx, acc.ID, Judgment{
		Kind:           sdk.OutcomeAccountDead,
		Reason:         "HTTP 401: invalid token",
		UpstreamStatus: http.StatusUnauthorized,
	})

	if len(recorder.events) != 1 {
		t.Fatalf("monitor events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Severity != monitoring.SeverityWarning {
		t.Fatalf("severity = %q, want warning", event.Severity)
	}
	if event.AccountNameSnapshot != "openai primary" {
		t.Fatalf("account_name_snapshot = %q, want openai primary", event.AccountNameSnapshot)
	}
	if event.Platform != "openai" {
		t.Fatalf("platform = %q, want openai", event.Platform)
	}
	if got := event.Detail["account_type"]; got != "oauth" {
		t.Fatalf("account_type detail = %#v, want oauth", got)
	}
}

func TestStateMachineSuccessClearsTransientAvoidanceExtra(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_transient_success")
	sm := NewStateMachine(db, nil)
	acc := createStateMachineAccount(ctx, db, "temporary 403", false)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	before := db.Account.GetX(ctx, acc.ID)
	assertShortDBAvoidance(t, before, 1, 7*time.Second, 8*time.Second)
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after success during transient window = %s, want degraded", fresh.State)
	}
	if fresh.StateUntil == nil || !fresh.StateUntil.Equal(*before.StateUntil) {
		t.Fatalf("state_until after success = %v, want %v", fresh.StateUntil, before.StateUntil)
	}
	if fresh.ErrorMsg != "" {
		t.Fatalf("error_msg after success = %q, want empty", fresh.ErrorMsg)
	}
	assertTransientAvoidanceExtraCleared(t, fresh)
}

func TestStateMachineFamilyTransientDoesNotChangeAccountState(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_family_transient_state")
	recorder := &captureMonitorRecorder{}
	sm := NewStateMachine(db, NewFamilyCooldown(nil))
	sm.monitor = recorder
	acc := createStateMachineAccount(ctx, db, "family overloaded", false)

	start := time.Now()
	sm.Apply(ctx, acc.ID, Judgment{
		Kind:           sdk.OutcomeFamilyTransient,
		Reason:         "Our servers are currently overloaded. Please try again later.",
		Family:         "openai:gpt-5",
		UpstreamStatus: http.StatusTooManyRequests,
		Duration:       123 * time.Millisecond,
	})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateActive {
		t.Fatalf("state after family transient = %s, want active", fresh.State)
	}
	if fresh.StateUntil != nil {
		t.Fatalf("state_until after family transient = %v, want nil", fresh.StateUntil)
	}
	if fresh.ErrorMsg != "" {
		t.Fatalf("error_msg after family transient = %q, want empty", fresh.ErrorMsg)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("monitor events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.ErrorCode != "account_family_transient" {
		t.Fatalf("error_code = %q, want account_family_transient", event.ErrorCode)
	}
	if got := event.Detail["family"]; got != "openai:gpt-5" {
		t.Fatalf("family detail = %#v, want openai:gpt-5", got)
	}
	if got := event.Detail["step"]; got != 1 {
		t.Fatalf("step detail = %#v, want 1", got)
	}
	if got := event.Detail["short_avoidance"]; got != true {
		t.Fatalf("short_avoidance detail = %#v, want true", got)
	}
	if event.AutoResolveAt == nil {
		t.Fatalf("auto_resolve_at should be set")
	}
	delay := event.AutoResolveAt.Sub(start)
	if delay < 7*time.Second || delay > 8*time.Second {
		t.Fatalf("family transient delay = %s, want about 7.5s", delay)
	}
}

func TestStateMachineSuccessClearsFamilyTransientStep(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_family_transient_success")
	rdb, mock := redismock.NewClientMock()
	sm := NewStateMachine(db, NewFamilyCooldown(rdb))
	acc := createStateMachineAccount(ctx, db, "family success", false)

	mock.ExpectDel(familyCooldownTransientStepKey(acc.ID, "openai:gpt-5")).SetVal(1)
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess, Family: "openai:gpt-5"})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestStateMachineSuccessDoesNotClearUnexpiredTemporaryStates(t *testing.T) {
	ctx := context.Background()

	t.Run("degraded", func(t *testing.T) {
		db := openStateMachineTestDB(t, "scheduler_success_preserves_degraded")
		sm := NewStateMachine(db, nil)
		acc := createStateMachineAccount(ctx, db, "temporary 403", false)
		applyJudgmentN(ctx, sm, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"}, 4)
		before := db.Account.GetX(ctx, acc.ID)
		if before.State != account.StateDegraded || before.StateUntil == nil {
			t.Fatalf("state before success = %s until %v, want degraded", before.State, before.StateUntil)
		}

		sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess})

		fresh := db.Account.GetX(ctx, acc.ID)
		if fresh.State != account.StateDegraded {
			t.Fatalf("state after success during degraded window = %s, want degraded", fresh.State)
		}
		if fresh.StateUntil == nil || !fresh.StateUntil.Equal(*before.StateUntil) {
			t.Fatalf("state_until after success = %v, want %v", fresh.StateUntil, before.StateUntil)
		}
		assertTransientAvoidanceExtraCleared(t, fresh)
		if fresh.LastUsedAt == nil {
			t.Fatalf("last_used_at should be updated after success")
		}
	})

	t.Run("rate_limited", func(t *testing.T) {
		db := openStateMachineTestDB(t, "scheduler_success_preserves_rate_limited")
		sm := NewStateMachine(db, nil)
		acc := createStateMachineAccount(ctx, db, "temporary 429", false)

		sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountRateLimited, RetryAfter: time.Minute, Reason: "HTTP 429"})
		before := db.Account.GetX(ctx, acc.ID)
		if before.State != account.StateRateLimited || before.StateUntil == nil {
			t.Fatalf("state before success = %s until %v, want rate_limited", before.State, before.StateUntil)
		}

		sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess})

		fresh := db.Account.GetX(ctx, acc.ID)
		if fresh.State != account.StateRateLimited {
			t.Fatalf("state after success during rate limit window = %s, want rate_limited", fresh.State)
		}
		if fresh.StateUntil == nil || !fresh.StateUntil.Equal(*before.StateUntil) {
			t.Fatalf("state_until after success = %v, want %v", fresh.StateUntil, before.StateUntil)
		}
		if fresh.LastUsedAt == nil {
			t.Fatalf("last_used_at should be updated after success")
		}
	})
}

func TestStateMachineDisabledIsNotOverwrittenByLateJudgments(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		j    Judgment
	}{
		{name: "success", j: Judgment{Kind: sdk.OutcomeSuccess}},
		{name: "rate limited", j: Judgment{Kind: sdk.OutcomeAccountRateLimited, RetryAfter: time.Minute, Reason: "HTTP 429"}},
		{name: "account unavailable", j: Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"}},
		{name: "upstream transient", j: Judgment{Kind: sdk.OutcomeUpstreamTransient, Reason: "HTTP 502"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openStateMachineTestDB(t, "scheduler_disabled_guard_"+tt.name)
			sm := NewStateMachine(db, nil)
			acc := db.Account.Create().
				SetName("manual disabled").
				SetPlatform("openai").
				SetType("oauth").
				SetCredentials(map[string]string{}).
				SetState(account.StateDisabled).
				SetErrorMsg("manual").
				SaveX(ctx)

			sm.Apply(ctx, acc.ID, tt.j)

			fresh := db.Account.GetX(ctx, acc.ID)
			if fresh.State != account.StateDisabled {
				t.Fatalf("state after %s = %s, want disabled", tt.name, fresh.State)
			}
			if fresh.StateUntil != nil {
				t.Fatalf("state_until after %s = %v, want nil", tt.name, fresh.StateUntil)
			}
			if fresh.ErrorMsg != "manual" {
				t.Fatalf("error_msg after %s = %q, want manual", tt.name, fresh.ErrorMsg)
			}
		})
	}
}

func TestStateMachineTransientAvoidanceDoesNotLoosenUnexpiredRateLimit(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_transient_preserves_rate_limited")
	sm := NewStateMachine(db, nil)
	acc := createStateMachineAccount(ctx, db, "temporary 429", false)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountRateLimited, RetryAfter: time.Minute, Reason: "HTTP 429"})
	before := db.Account.GetX(ctx, acc.ID)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeUpstreamTransient, Reason: "HTTP 502"})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateRateLimited {
		t.Fatalf("state after transient = %s, want rate_limited", fresh.State)
	}
	if fresh.StateUntil == nil || !fresh.StateUntil.Equal(*before.StateUntil) {
		t.Fatalf("state_until after transient = %v, want %v", fresh.StateUntil, before.StateUntil)
	}
	if extraInt(fresh.Extra, transientAvoidStepExtraKey) != 0 {
		t.Fatalf("transient step should not be set while rate_limited: %+v", fresh.Extra)
	}
}

func TestStateMachineTransientAvoidanceTreatsExpiredRateLimitAsActive(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_transient_after_expired_rate_limit")
	sm := NewStateMachine(db, nil)
	acc := db.Account.Create().
		SetName("expired 429").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SetState(account.StateRateLimited).
		SetStateUntil(time.Now().Add(-time.Second)).
		SetErrorMsg("HTTP 429").
		SaveX(ctx)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})

	fresh := db.Account.GetX(ctx, acc.ID)
	assertShortDBAvoidance(t, fresh, 1, 7*time.Second, 8*time.Second)
}

func TestSchedulabilityWithTransientAvoidanceKeepsPlainDegradedStickyOnly(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Minute)
	acc := &ent.Account{
		State:      account.StateDegraded,
		StateUntil: &until,
		Extra:      map[string]interface{}{},
	}

	if got := SchedulabilityOf(acc, now); got != StickyOnly {
		t.Fatalf("plain degraded schedulability = %v, want StickyOnly", got)
	}
	if got := schedulabilityWithTransientAvoidance(acc, now); got != StickyOnly {
		t.Fatalf("plain transient-aware degraded schedulability = %v, want StickyOnly", got)
	}
	if got := hardAffinitySchedulabilityWithTransientAvoidance(acc, now); got != StickyOnly {
		t.Fatalf("plain hard-affinity degraded schedulability = %v, want StickyOnly", got)
	}
}

func TestStateMachinePool403AvoidsWithoutDisable(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_pool_403_avoidance")
	sm := NewStateMachine(db, nil)
	acc := createStateMachineAccount(ctx, db, "pool", true)

	applyJudgmentN(ctx, sm, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"}, 8)

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State == account.StateDisabled {
		t.Fatalf("pool account should not be disabled after 403 avoidance")
	}
	if fresh.State != account.StateDegraded {
		t.Fatalf("pool account state = %s, want degraded", fresh.State)
	}
}

func TestStateMachineUpstreamTransientAvoidsWithoutDisable(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_5xx_avoidance")
	sm := NewStateMachine(db, nil)
	acc := createStateMachineAccount(ctx, db, "upstream 502", false)

	applyJudgmentN(ctx, sm, acc.ID, Judgment{Kind: sdk.OutcomeUpstreamTransient, Reason: "HTTP 502"}, 8)

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State == account.StateDisabled {
		t.Fatalf("5xx should never disable account")
	}
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after repeated 5xx = %s, want degraded", fresh.State)
	}
}

func openStateMachineTestDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	db := testdb.OpenMemoryEnt(t, name, migrate.WithGlobalUniqueID(false))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}

func createStateMachineAccount(ctx context.Context, db *ent.Client, name string, isPool bool) *ent.Account {
	return db.Account.Create().
		SetName(name).
		SetPlatform("openai").
		SetType("oauth").
		SetUpstreamIsPool(isPool).
		SetCredentials(map[string]string{}).
		SaveX(ctx)
}

func applyJudgmentN(ctx context.Context, sm *StateMachine, accountID int, j Judgment, n int) {
	for i := 0; i < n; i++ {
		sm.Apply(ctx, accountID, j)
	}
}

func assertShortDBAvoidance(t *testing.T, acc *ent.Account, wantStep int, minDelay time.Duration, maxDelay time.Duration) {
	t.Helper()
	now := time.Now()
	if acc.State != account.StateDegraded {
		t.Fatalf("state = %s, want degraded", acc.State)
	}
	if got := extraInt(acc.Extra, transientAvoidStepExtraKey); got != wantStep {
		t.Fatalf("avoid step = %d, want %d", got, wantStep)
	}
	if !isTransientAvoidanceWindow(acc, now) {
		t.Fatalf("account should be in transient avoidance window: state=%s until=%v extra=%+v", acc.State, acc.StateUntil, acc.Extra)
	}
	if got := schedulabilityWithTransientAvoidance(acc, now); got != NotSchedulable {
		t.Fatalf("transient schedulability = %v, want NotSchedulable", got)
	}
	if got := hardAffinitySchedulabilityWithTransientAvoidance(acc, now); got != NotSchedulable {
		t.Fatalf("hard-affinity transient schedulability = %v, want NotSchedulable", got)
	}
	if acc.StateUntil == nil {
		t.Fatalf("degraded state_until missing")
	}
	delay := acc.StateUntil.Sub(now)
	if delay < minDelay || delay > maxDelay {
		t.Fatalf("avoid delay = %s, want between %s and %s", delay, minDelay, maxDelay)
	}
}

func assertTransientAvoidanceExtraCleared(t *testing.T, acc *ent.Account) {
	t.Helper()
	if hasTransientAvoidanceExtra(acc.Extra) {
		t.Fatalf("transient avoidance extra should be cleared: %+v", acc.Extra)
	}
}

func assertDBDegraded(t *testing.T, acc *ent.Account, minDelay time.Duration, maxDelay time.Duration) {
	t.Helper()
	now := time.Now()
	if got := SchedulabilityOf(acc, now); got != StickyOnly {
		t.Fatalf("degraded schedulability = %v, want StickyOnly", got)
	}
	if got := schedulabilityWithTransientAvoidance(acc, now); got != NotSchedulable {
		t.Fatalf("transient-aware degraded schedulability = %v, want NotSchedulable", got)
	}
	if got := hardAffinitySchedulabilityWithTransientAvoidance(acc, now); got != NotSchedulable {
		t.Fatalf("hard-affinity transient-aware degraded schedulability = %v, want NotSchedulable", got)
	}
	if got := extraInt(acc.Extra, transientAvoidStepExtraKey); got != 4 {
		t.Fatalf("avoid step = %d, want 4", got)
	}
	if acc.StateUntil == nil {
		t.Fatalf("degraded state_until missing")
	}
	delay := acc.StateUntil.Sub(now)
	if delay < minDelay || delay > maxDelay {
		t.Fatalf("degraded delay = %s, want between %s and %s", delay, minDelay, maxDelay)
	}
}
