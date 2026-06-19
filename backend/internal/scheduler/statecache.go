package scheduler

import (
	"sync"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
)

type accountStateSnapshot struct {
	State      account.State
	StateUntil *time.Time
	Extra      map[string]interface{}
}

type accountStateCache struct {
	m sync.Map
}

func newAccountStateCache() *accountStateCache {
	return &accountStateCache{}
}

func (c *accountStateCache) Store(accountID int, state account.State, stateUntil *time.Time, extra map[string]interface{}) {
	if c == nil || accountID <= 0 {
		return
	}
	var until *time.Time
	if stateUntil != nil {
		value := *stateUntil
		until = &value
	}
	var snapshotExtra map[string]interface{}
	if extra != nil {
		snapshotExtra = cloneExtra(extra)
	}
	c.m.Store(accountID, accountStateSnapshot{
		State:      state,
		StateUntil: until,
		Extra:      snapshotExtra,
	})
}

func (c *accountStateCache) Delete(accountID int) {
	if c == nil || accountID <= 0 {
		return
	}
	c.m.Delete(accountID)
}

func (c *accountStateCache) Apply(acc *ent.Account) *ent.Account {
	if c == nil || acc == nil {
		return acc
	}
	value, ok := c.m.Load(acc.ID)
	if !ok {
		return acc
	}
	snapshot := value.(accountStateSnapshot)
	cloned := *acc
	cloned.State = snapshot.State
	cloned.StateUntil = snapshot.StateUntil
	if snapshot.Extra != nil {
		cloned.Extra = cloneExtra(snapshot.Extra)
	}
	return &cloned
}
