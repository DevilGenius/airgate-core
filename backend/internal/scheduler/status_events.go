package scheduler

import "time"

// AccountStatusEventPublisher receives best-effort account status changes.
// Status includes the persisted scheduling state and Redis model-family
// cooldowns shown by the credentials management page.
type AccountStatusEventPublisher interface {
	PublishAccountStateChanged(accountID int, state string, stateUntil *time.Time, errorMsg string)
	PublishAccountFamilyCooldownChanged(accountID int, action, family string, until *time.Time, reason string)
}

// SetAccountStatusEventPublisher injects a best-effort status event publisher.
func (s *Scheduler) SetAccountStatusEventPublisher(publisher AccountStatusEventPublisher) {
	if s == nil || s.state == nil {
		return
	}
	s.state.statusPublisher = publisher
}

func (sm *StateMachine) publishAccountStateChanged(accountID int, state string, stateUntil *time.Time, errorMsg string) {
	if sm == nil || sm.statusPublisher == nil || accountID <= 0 {
		return
	}
	sm.statusPublisher.PublishAccountStateChanged(accountID, state, stateUntil, errorMsg)
}

func (sm *StateMachine) publishAccountFamilyCooldownUpsert(accountID int, family string, until time.Time, reason string) {
	if sm == nil || sm.statusPublisher == nil || accountID <= 0 || family == "" {
		return
	}
	sm.statusPublisher.PublishAccountFamilyCooldownChanged(accountID, "upsert", family, &until, reason)
}

func (sm *StateMachine) publishAccountFamilyCooldownClear(accountID int) {
	if sm == nil || sm.statusPublisher == nil || accountID <= 0 {
		return
	}
	sm.statusPublisher.PublishAccountFamilyCooldownChanged(accountID, "clear", "", nil, "")
}
