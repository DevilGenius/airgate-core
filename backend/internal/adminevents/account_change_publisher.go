package adminevents

import (
	"context"
	"sync"
	"time"
)

const defaultAccountChangeFlushInterval = 350 * time.Millisecond

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

// AccountChangePublisher keeps only the latest pending account-field
// snapshots before publishing them to the shared admin event hub. This keeps
// request-path status changes convergent without making event volume depend on
// request volume.
type AccountChangePublisher struct {
	hub      *Hub
	interval time.Duration

	mu       sync.Mutex
	patches  map[int]AccountPatch
	families map[accountFamilyKey]pendingFamilyCooldown
}

// NewAccountChangePublisher creates an account-change publisher whose Run
// method must be attached to the server lifecycle.
func NewAccountChangePublisher(hub *Hub) *AccountChangePublisher {
	return &AccountChangePublisher{
		hub:      hub,
		interval: defaultAccountChangeFlushInterval,
		patches:  make(map[int]AccountPatch),
		families: make(map[accountFamilyKey]pendingFamilyCooldown),
	}
}

// PublishAccountStateChanged coalesces repeated state snapshots by account.
func (p *AccountChangePublisher) PublishAccountStateChanged(
	accountID int,
	state string,
	stateUntil *time.Time,
	errorMsg string,
) {
	if p == nil || p.hub == nil || accountID <= 0 {
		return
	}

	until := ""
	if stateUntil != nil {
		until = stateUntil.UTC().Format(time.RFC3339Nano)
	}
	p.PublishAccountChanged(accountID, AccountPatch{AccountState: state, StateUntil: &until, ErrorMsg: &errorMsg})
}

// PublishAccountChanged merges independent fields without losing preceding updates.
func (p *AccountChangePublisher) PublishAccountChanged(accountID int, patch AccountPatch) {
	if p == nil || p.hub == nil || accountID <= 0 {
		return
	}
	p.mu.Lock()
	p.patches[accountID] = p.patches[accountID].Merge(patch)
	p.mu.Unlock()
}

// PublishAccountFamilyCooldownChanged coalesces family updates by account and
// family. A clear removes earlier pending upserts for the account; upserts that
// arrive after a clear are retained and published after that clear.
func (p *AccountChangePublisher) PublishAccountFamilyCooldownChanged(
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
func (p *AccountChangePublisher) Run(ctx context.Context) {
	if p == nil || p.hub == nil || ctx == nil {
		return
	}

	interval := p.interval
	if interval <= 0 {
		interval = defaultAccountChangeFlushInterval
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

func (p *AccountChangePublisher) flush() {
	if p == nil || p.hub == nil {
		return
	}

	p.mu.Lock()
	if len(p.patches) == 0 && len(p.families) == 0 {
		p.mu.Unlock()
		return
	}
	patches := p.patches
	families := p.families
	p.patches = make(map[int]AccountPatch)
	p.families = make(map[accountFamilyKey]pendingFamilyCooldown)
	p.mu.Unlock()

	for accountID, pending := range patches {
		if pending.AccountState == "" && pending.Priority == nil && pending.SchedulingWeight == nil && pending.MaxConcurrency == nil && pending.Cognition == nil && pending.ModelDowngradeThreshold == nil {
			continue
		}
		p.hub.PublishAccountChanged(accountID, pending)
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
