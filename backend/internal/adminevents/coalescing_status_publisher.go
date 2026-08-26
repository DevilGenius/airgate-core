package adminevents

import (
	"context"
	"sync"
	"time"
)

const defaultAccountStatusFlushInterval = 350 * time.Millisecond

type pendingAccountState struct {
	state      string
	stateUntil *time.Time
	errorMsg   string
}

type accountFamilyKey struct {
	accountID int
	family    string
}

type pendingFamilyCooldown struct {
	action     string
	until      *time.Time
	reason     string
	durationMs int64
}

// CoalescingStatusPublisher keeps only the latest pending account-status
// snapshots before publishing them to the shared admin event hub. This keeps
// request-path status changes convergent without making event volume depend on
// request volume.
type CoalescingStatusPublisher struct {
	hub      *Hub
	interval time.Duration

	mu       sync.Mutex
	states   map[int]pendingAccountState
	families map[accountFamilyKey]pendingFamilyCooldown
}

// NewCoalescingStatusPublisher creates an account-status publisher whose Run
// method must be attached to the server lifecycle.
func NewCoalescingStatusPublisher(hub *Hub) *CoalescingStatusPublisher {
	return &CoalescingStatusPublisher{
		hub:      hub,
		interval: defaultAccountStatusFlushInterval,
		states:   make(map[int]pendingAccountState),
		families: make(map[accountFamilyKey]pendingFamilyCooldown),
	}
}

// PublishAccountStateChanged coalesces repeated state snapshots by account.
func (p *CoalescingStatusPublisher) PublishAccountStateChanged(
	accountID int,
	state string,
	stateUntil *time.Time,
	errorMsg string,
) {
	if p == nil || p.hub == nil || accountID <= 0 {
		return
	}

	p.mu.Lock()
	p.states[accountID] = pendingAccountState{
		state:      state,
		stateUntil: cloneTime(stateUntil),
		errorMsg:   errorMsg,
	}
	p.mu.Unlock()
}

// PublishAccountFamilyCooldownChanged coalesces family updates by account and
// family. A clear removes earlier pending upserts for the account; upserts that
// arrive after a clear are retained and published after that clear.
func (p *CoalescingStatusPublisher) PublishAccountFamilyCooldownChanged(
	accountID int,
	action string,
	family string,
	until *time.Time,
	reason string,
	durationMs int64,
) {
	if p == nil || p.hub == nil || accountID <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	switch action {
	case "clear":
		for key := range p.families {
			if key.accountID == accountID {
				delete(p.families, key)
			}
		}
		p.families[accountFamilyKey{accountID: accountID}] = pendingFamilyCooldown{action: action}
	case "upsert":
		if family == "" {
			return
		}
		p.families[accountFamilyKey{accountID: accountID, family: family}] = pendingFamilyCooldown{
			action:     action,
			until:      cloneTime(until),
			reason:     reason,
			durationMs: durationMs,
		}
	}
}

// Run flushes coalesced events until ctx is canceled, then performs one final
// flush so terminal values queued during shutdown are not left behind.
func (p *CoalescingStatusPublisher) Run(ctx context.Context) {
	if p == nil || p.hub == nil || ctx == nil {
		return
	}

	interval := p.interval
	if interval <= 0 {
		interval = defaultAccountStatusFlushInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.flush()
		case <-ctx.Done():
			p.flush()
			return
		}
	}
}

func (p *CoalescingStatusPublisher) flush() {
	if p == nil || p.hub == nil {
		return
	}

	p.mu.Lock()
	if len(p.states) == 0 && len(p.families) == 0 {
		p.mu.Unlock()
		return
	}
	states := p.states
	families := p.families
	p.states = make(map[int]pendingAccountState)
	p.families = make(map[accountFamilyKey]pendingFamilyCooldown)
	p.mu.Unlock()

	for accountID, pending := range states {
		p.hub.PublishAccountStateChanged(accountID, pending.state, pending.stateUntil, pending.errorMsg)
	}

	for key, pending := range families {
		if pending.action == "clear" {
			p.hub.PublishAccountFamilyCooldownChanged(key.accountID, pending.action, "", nil, "", 0)
		}
	}
	for key, pending := range families {
		if pending.action == "upsert" {
			p.hub.PublishAccountFamilyCooldownChanged(key.accountID, pending.action, key.family, pending.until, pending.reason, pending.durationMs)
		}
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
