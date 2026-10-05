package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/adminevents"

	"github.com/DevilGenius/airgate-core/ent/account"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type recordingAccountStatusPublisher struct {
	accountChanges     int
	stateAccountIDs    []int
	stateValues        []string
	cooldownAccountIDs []int
}

func (p *recordingAccountStatusPublisher) PublishAccountStateChanged(accountID int, state string, _ *time.Time, _ string) {
	p.stateAccountIDs = append(p.stateAccountIDs, accountID)
	p.stateValues = append(p.stateValues, state)
}

func (p *recordingAccountStatusPublisher) PublishAccountFamilyCooldownChanged(accountID int, _, _ string, _ *time.Time, _ string, _ int64) {
	p.cooldownAccountIDs = append(p.cooldownAccountIDs, accountID)
}

func TestShortTransientAvoidancePublishesBackoffState(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_short_avoidance_status_event")
	sm := NewStateMachine(db, nil)
	publisher := &recordingAccountStatusPublisher{}
	sm.accountPublisher = publisher
	testAccount := createStateMachineAccount(ctx, db, "short avoidance event", false)

	sm.Apply(ctx, testAccount.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	if len(publisher.stateValues) != 0 {
		t.Fatalf("first transient observation published states = %v, want none", publisher.stateValues)
	}

	sm.Apply(ctx, testAccount.ID, Judgment{Kind: sdk.OutcomeAccountUnavailable, Reason: "HTTP 403"})
	if len(publisher.stateValues) != 1 || publisher.stateAccountIDs[0] != testAccount.ID || publisher.stateValues[0] != string(account.StateDegraded) {
		t.Fatalf("short transient avoidance events = ids=%v states=%v, want account %d degraded", publisher.stateAccountIDs, publisher.stateValues, testAccount.ID)
	}
}

func TestAccountEventPublisher(t *testing.T) {
	var nilScheduler *Scheduler
	nilScheduler.SetAccountEventPublisher(&recordingAccountStatusPublisher{})

	s := NewScheduler(nil, nil)
	publisher := &recordingAccountStatusPublisher{}
	s.SetAccountEventPublisher(publisher)
	s.state.publishAccountStateChanged(0, "active", nil, "")
	s.state.publishAccountStateChanged(7, "active", nil, "")
	s.state.publishAccountFamilyCooldownUpsert(8, "gpt-5.6-sol", time.Now().Add(time.Hour), "limited", time.Hour)
	s.state.publishAccountFamilyCooldownClear(9)

	if len(publisher.stateAccountIDs) != 1 || publisher.stateAccountIDs[0] != 7 {
		t.Fatalf("published state account IDs = %v, want [7]", publisher.stateAccountIDs)
	}
	if len(publisher.cooldownAccountIDs) != 2 || publisher.cooldownAccountIDs[0] != 8 || publisher.cooldownAccountIDs[1] != 9 {
		t.Fatalf("published cooldown account IDs = %v, want [8 9]", publisher.cooldownAccountIDs)
	}
}

func (p *recordingAccountStatusPublisher) PublishAccountChanged(int, adminevents.AccountPatch) {
	p.accountChanges++
}

func TestModelOutcomeBurstDoesNotPublishAccountEvents(t *testing.T) {
	publisher := &recordingAccountStatusPublisher{}
	scheduler := &Scheduler{state: &StateMachine{}, modelSuccessRate: NewModelSuccessRateTracker(nil)}
	scheduler.SetAccountEventPublisher(publisher)
	for id := 1; id <= 300; id++ {
		for range 100 {
			scheduler.RecordModelOutcome(id, "gpt", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
		}
		if len(scheduler.ListModelDemotions(id, .9)) != 1 {
			t.Fatalf("account %d lost its model statistics", id)
		}
	}
	if publisher.accountChanges != 0 || len(publisher.stateAccountIDs) != 0 || len(publisher.cooldownAccountIDs) != 0 {
		t.Fatal("request statistics must not enqueue account SSE events")
	}
	scheduler.state.publishAccountStateChanged(1, "disabled", nil, "manual")
	if len(publisher.stateAccountIDs) != 1 {
		t.Fatal("state publishing stopped")
	}
}
