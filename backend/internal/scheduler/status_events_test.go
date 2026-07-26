package scheduler

import (
	"testing"
	"time"
)

type recordingAccountStatusPublisher struct {
	stateAccountIDs    []int
	cooldownAccountIDs []int
}

func (p *recordingAccountStatusPublisher) PublishAccountStateChanged(accountID int, _ string, _ *time.Time, _ string) {
	p.stateAccountIDs = append(p.stateAccountIDs, accountID)
}

func (p *recordingAccountStatusPublisher) PublishAccountFamilyCooldownChanged(accountID int, _, _ string, _ *time.Time, _ string) {
	p.cooldownAccountIDs = append(p.cooldownAccountIDs, accountID)
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
