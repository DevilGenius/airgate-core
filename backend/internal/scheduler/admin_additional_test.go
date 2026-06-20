package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent/account"
)

func TestSchedulerAdminStateWriteEntrypoints(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_admin_state_write")
	s := &Scheduler{
		db:             db,
		state:          NewStateMachine(db, nil),
		stateCache:     newAccountStateCache(),
		familyCooldown: &facadeFamilyCooldownTracker{clear: 2},
	}

	until := time.Now().Add(time.Hour).UTC()
	recoverAccount := db.Account.Create().
		SetName("recover").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SetState(account.StateDegraded).
		SetStateUntil(until).
		SetErrorMsg("temporary").
		SetExtra(map[string]interface{}{transientAvoidStepExtraKey: 3, "keep": "value"}).
		SaveX(ctx)

	if err := s.ManualRecover(ctx, recoverAccount.ID); err != nil {
		t.Fatalf("ManualRecover() error = %v", err)
	}
	fresh := db.Account.GetX(ctx, recoverAccount.ID)
	if fresh.State != account.StateActive || fresh.StateUntil != nil || fresh.ErrorMsg != "" {
		t.Fatalf("manual recovered account = state %s until %v err %q", fresh.State, fresh.StateUntil, fresh.ErrorMsg)
	}
	if hasTransientAvoidanceExtra(fresh.Extra) || fresh.Extra["keep"] != "value" {
		t.Fatalf("manual recover extra = %+v", fresh.Extra)
	}

	if err := s.ManualRecover(ctx, 999999); err == nil {
		t.Fatal("ManualRecover(missing) error = nil")
	}

	disableAccount := db.Account.Create().
		SetName("disable").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SaveX(ctx)
	longReason := strings.Repeat("x", 500)
	if err := s.ManualDisable(ctx, disableAccount.ID, longReason); err != nil {
		t.Fatalf("ManualDisable() error = %v", err)
	}
	fresh = db.Account.GetX(ctx, disableAccount.ID)
	if fresh.State != account.StateDisabled || fresh.StateUntil != nil || fresh.ErrorMsg == "" || len(fresh.ErrorMsg) > 500 {
		t.Fatalf("manual disabled account = state %s until %v err len %d", fresh.State, fresh.StateUntil, len(fresh.ErrorMsg))
	}
	if err := s.ManualDisable(ctx, 999998, "missing"); err == nil {
		t.Fatal("ManualDisable(missing) error = nil")
	}

	limited := db.Account.Create().
		SetName("limited").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SaveX(ctx)
	limitUntil := time.Now().Add(2 * time.Minute)
	s.MarkRateLimited(ctx, limited.ID, limitUntil, "quota")
	fresh = db.Account.GetX(ctx, limited.ID)
	if fresh.State != account.StateRateLimited || fresh.StateUntil == nil || fresh.ErrorMsg != "quota" {
		t.Fatalf("rate limited account = state %s until %v err %q", fresh.State, fresh.StateUntil, fresh.ErrorMsg)
	}
	cleared := s.ClearRateLimitMarkers(ctx, limited.ID)
	if cleared != 3 {
		t.Fatalf("ClearRateLimitMarkers() = %d, want family 2 + state 1", cleared)
	}
	fresh = db.Account.GetX(ctx, limited.ID)
	if fresh.State != account.StateActive || fresh.StateUntil != nil {
		t.Fatalf("cleared account = state %s until %v", fresh.State, fresh.StateUntil)
	}
	if got := s.ClearRateLimitMarkers(ctx, 999997); got != 2 {
		t.Fatalf("ClearRateLimitMarkers(missing) = %d, want family cooldown count only", got)
	}

	disabled := db.Account.Create().
		SetName("auto disabled").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SaveX(ctx)
	s.MarkDisabled(ctx, disabled.ID, "invalid token")
	fresh = db.Account.GetX(ctx, disabled.ID)
	if fresh.State != account.StateDisabled || fresh.ErrorMsg != "invalid token" {
		t.Fatalf("MarkDisabled account = state %s err %q", fresh.State, fresh.ErrorMsg)
	}

	degraded := db.Account.Create().
		SetName("auto degraded").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SaveX(ctx)
	s.MarkDegraded(ctx, degraded.ID, "HTTP 403")
	fresh = db.Account.GetX(ctx, degraded.ID)
	if fresh.State != account.StateDegraded || fresh.StateUntil == nil || !hasTransientAvoidanceExtra(fresh.Extra) {
		t.Fatalf("MarkDegraded account = state %s until %v extra %+v", fresh.State, fresh.StateUntil, fresh.Extra)
	}

	s.ClearRateLimited(ctx, degraded.ID)
	fresh = db.Account.GetX(ctx, degraded.ID)
	if fresh.State != account.StateActive || fresh.StateUntil != nil {
		t.Fatalf("ClearRateLimited account = state %s until %v", fresh.State, fresh.StateUntil)
	}
}
