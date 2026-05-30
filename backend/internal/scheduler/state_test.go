package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/ent/migrate"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestStateMachineAccountUnavailableEscalatesAfterThreshold(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_unavailable_threshold")
	sm := NewStateMachine(db, nil, nil)
	criticalTransitions := 0
	sm.onCriticalTransition = func() { criticalTransitions++ }

	acc := db.Account.Create().
		SetName("temporary 403").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SaveX(ctx)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after first unavailable = %s, want degraded", fresh.State)
	}
	if fresh.StateUntil == nil {
		t.Fatalf("state_until should be set after first unavailable")
	}
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 1 {
		t.Fatalf("unavailable count after first unavailable = %d, want 1", got)
	}

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh = db.Account.GetX(ctx, acc.ID)
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 1 {
		t.Fatalf("unavailable count during degraded window = %d, want 1", got)
	}

	expireAccountDegradedWindow(ctx, db, acc.ID)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh = db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after second unavailable = %s, want degraded", fresh.State)
	}
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 2 {
		t.Fatalf("unavailable count after second unavailable = %d, want 2", got)
	}

	expireAccountDegradedWindow(ctx, db, acc.ID)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	fresh = db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDisabled {
		t.Fatalf("state after third unavailable = %s, want disabled", fresh.State)
	}
	if fresh.StateUntil != nil {
		t.Fatalf("state_until should be cleared after escalation")
	}
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 0 {
		t.Fatalf("unavailable count after escalation = %d, want cleared", got)
	}
	if criticalTransitions != 1 {
		t.Fatalf("critical transitions = %d, want 1", criticalTransitions)
	}
}

func TestStateMachineSuccessClearsAccountUnavailableCount(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_unavailable_success")
	sm := NewStateMachine(db, nil, nil)

	acc := db.Account.Create().
		SetName("temporary 403").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		SetExtra(map[string]interface{}{"keep": "value"}).
		SaveX(ctx)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeSuccess})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateActive {
		t.Fatalf("state after success = %s, want active", fresh.State)
	}
	if fresh.StateUntil != nil {
		t.Fatalf("state_until should be cleared after success")
	}
	if fresh.ErrorMsg != "" {
		t.Fatalf("error_msg after success = %q, want empty", fresh.ErrorMsg)
	}
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 0 {
		t.Fatalf("unavailable count after success = %d, want cleared", got)
	}
	if fresh.Extra["keep"] != "value" {
		t.Fatalf("unrelated extra value was not preserved: %+v", fresh.Extra)
	}
}

func TestShouldTrackAccountUnavailableOnlyNonPool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		acc  *ent.Account
		want bool
	}{
		{
			name: "oauth account",
			acc:  &ent.Account{Type: "oauth"},
			want: true,
		},
		{
			name: "oauth pool account",
			acc:  &ent.Account{Type: "oauth", UpstreamIsPool: true},
			want: false,
		},
		{
			name: "api key account",
			acc:  &ent.Account{Type: "apikey"},
			want: true,
		},
		{
			name: "api key pool account",
			acc:  &ent.Account{Type: "apikey", UpstreamIsPool: true},
			want: false,
		},
		{
			name: "nil account",
			acc:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldTrackAccountUnavailable(tt.acc); got != tt.want {
				t.Fatalf("shouldTrackAccountUnavailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStateMachineAccountUnavailableTracksNormalAPIKey(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_unavailable_api_key")
	sm := NewStateMachine(db, nil, nil)

	acc := db.Account.Create().
		SetName("api key").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{}).
		SaveX(ctx)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateDegraded {
		t.Fatalf("state after api key unavailable = %s, want degraded", fresh.State)
	}
	if fresh.StateUntil == nil {
		t.Fatalf("state_until should be set for normal api key account")
	}
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 1 {
		t.Fatalf("unavailable count for normal api key account = %d, want 1", got)
	}
}

func TestStateMachineAccountUnavailableIgnoredForAPIPool(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_unavailable_api_pool")
	sm := NewStateMachine(db, nil, nil)

	acc := db.Account.Create().
		SetName("api pool").
		SetPlatform("openai").
		SetType("apikey").
		SetUpstreamIsPool(true).
		SetCredentials(map[string]string{}).
		SaveX(ctx)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateActive {
		t.Fatalf("state after api pool unavailable = %s, want active", fresh.State)
	}
	if fresh.StateUntil != nil {
		t.Fatalf("state_until should remain empty for api pool account")
	}
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 0 {
		t.Fatalf("unavailable count for api pool account = %d, want 0", got)
	}
}

func TestStateMachineAccountUnavailableIgnoredForOAuthPool(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_account_unavailable_oauth_pool")
	sm := NewStateMachine(db, nil, nil)

	acc := db.Account.Create().
		SetName("oauth pool").
		SetPlatform("openai").
		SetType("oauth").
		SetUpstreamIsPool(true).
		SetCredentials(map[string]string{}).
		SaveX(ctx)

	sm.Apply(ctx, acc.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})

	fresh := db.Account.GetX(ctx, acc.ID)
	if fresh.State != account.StateActive {
		t.Fatalf("state after oauth pool unavailable = %s, want active", fresh.State)
	}
	if fresh.StateUntil != nil {
		t.Fatalf("state_until should remain empty for oauth pool account")
	}
	if got := extraInt(fresh.Extra, accountUnavailableCountExtraKey); got != 0 {
		t.Fatalf("unavailable count for oauth pool account = %d, want 0", got)
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

func expireAccountDegradedWindow(ctx context.Context, db *ent.Client, accountID int) {
	db.Account.UpdateOneID(accountID).
		SetStateUntil(time.Now().Add(-time.Second)).
		ExecX(ctx)
}
