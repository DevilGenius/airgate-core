package scheduler

import "time"

// AccountEventPublisher receives best-effort account status changes.
// Status includes the persisted scheduling state and Redis model-family
// cooldowns shown by the credentials management page.
type AccountEventPublisher interface {
	PublishAccountStateChanged(accountID int, state string, stateUntil *time.Time, errorMsg string)
	PublishAccountFamilyCooldownChanged(accountID int, action, family string, until *time.Time, reason string, durationMs int64)
}

// SetAccountEventPublisher injects a best-effort status event publisher.
func (s *Scheduler) SetAccountEventPublisher(publisher AccountEventPublisher) {
	if s == nil || s.state == nil {
		return
	}
	s.state.accountPublisher = publisher
}

func (sm *StateMachine) publishAccountStateChanged(accountID int, state string, stateUntil *time.Time, errorMsg string) {
	if sm == nil || sm.accountPublisher == nil || accountID <= 0 {
		return
	}
	sm.accountPublisher.PublishAccountStateChanged(accountID, state, stateUntil, errorMsg)
}

func (sm *StateMachine) publishAccountFamilyCooldownUpsert(accountID int, family string, until time.Time, reason string, window time.Duration) {
	if sm == nil || sm.accountPublisher == nil || accountID <= 0 || family == "" {
		return
	}
	sm.accountPublisher.PublishAccountFamilyCooldownChanged(accountID, "upsert", family, &until, reason, window.Milliseconds())
}

func (sm *StateMachine) publishAccountFamilyCooldownClear(accountID int) {
	if sm == nil || sm.accountPublisher == nil || accountID <= 0 {
		return
	}
	sm.accountPublisher.PublishAccountFamilyCooldownChanged(accountID, "clear", "", nil, "", 0)
}
