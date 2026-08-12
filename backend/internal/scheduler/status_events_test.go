package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent/account"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type recordingAccountStatusPublisher struct {
	stateAccountIDs    []int
	stateValues        []string
	cooldownAccountIDs []int
}

func (p *recordingAccountStatusPublisher) PublishAccountStateChanged(accountID int, state string, _ *time.Time, _ string) {
	p.stateAccountIDs = append(p.stateAccountIDs, accountID)
	p.stateValues = append(p.stateValues, state)
}

func (p *recordingAccountStatusPublisher) PublishAccountFamilyCooldownChanged(accountID int, _, _ string, _ *time.Time, _ string) {
	p.cooldownAccountIDs = append(p.cooldownAccountIDs, accountID)
}

func TestShortTransientAvoidancePublishesBackoffState(t *testing.T) {
	ctx := context.Background()
	db := openStateMachineTestDB(t, "scheduler_short_avoidance_status_event")
	sm := NewStateMachine(db, nil)
	publisher := &recordingAccountStatusPublisher{}
	sm.statusPublisher = publisher
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

func TestAccountStatusEventPublisher(t *testing.T) {
	var nilScheduler *Scheduler
	nilScheduler.SetAccountStatusEventPublisher(&recordingAccountStatusPublisher{})

	s := NewScheduler(nil, nil)
	publisher := &recordingAccountStatusPublisher{}
	s.SetAccountStatusEventPublisher(publisher)
	s.state.publishAccountStateChanged(0, "active", nil, "")
	s.state.publishAccountStateChanged(7, "active", nil, "")
	s.state.publishAccountFamilyCooldownUpsert(8, "gpt-5.6-sol", time.Now().Add(time.Hour), "limited")
	s.state.publishAccountFamilyCooldownClear(9)

	if len(publisher.stateAccountIDs) != 1 || publisher.stateAccountIDs[0] != 7 {
		t.Fatalf("published state account IDs = %v, want [7]", publisher.stateAccountIDs)
	}
	if len(publisher.cooldownAccountIDs) != 2 || publisher.cooldownAccountIDs[0] != 8 || publisher.cooldownAccountIDs[1] != 9 {
		t.Fatalf("published cooldown account IDs = %v, want [8 9]", publisher.cooldownAccountIDs)
	}
}
