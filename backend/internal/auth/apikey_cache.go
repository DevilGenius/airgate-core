package auth

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

var ErrAPIKeyLookupBusy = errors.New("认证服务繁忙，请稍后重试")

type localKeyItem struct {
	key   string
	entry apiKeyCacheEntry
}
type localKeyCache struct {
	mu       sync.Mutex
	entries  map[string]*list.Element
	order    list.List
	capacity int
	timer    *time.Timer
}

func newLocalKeyCache(capacity int) *localKeyCache {
	return &localKeyCache{entries: make(map[string]*list.Element), capacity: capacity}
}

func (c *localKeyCache) Load(key any) (any, bool) {
	hash, ok := key.(string)
	if !ok {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[hash]; e != nil {
		c.order.MoveToFront(e)
		return e.Value.(localKeyItem).entry, true
	}
	return nil, false
}

func (c *localKeyCache) Store(key any, value apiKeyCacheEntry) {
	hash, ok := key.(string)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[hash]; e != nil {
		e.Value = localKeyItem{hash, value}
		c.order.MoveToFront(e)
	} else {
		if len(c.entries) >= c.capacity {
			e := c.order.Back()
			delete(c.entries, e.Value.(localKeyItem).key)
			c.order.Remove(e)
		}
		c.entries[hash] = c.order.PushFront(localKeyItem{hash, value})
	}
	if c.timer == nil {
		c.timer = time.AfterFunc(apiKeyCacheTTL, c.prune)
	}
}

func (c *localKeyCache) Delete(key any) {
	hash, ok := key.(string)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[hash]; e != nil {
		delete(c.entries, hash)
		c.order.Remove(e)
	}
}

func (c *localKeyCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	c.order.Init()
	// Keep a scheduled callback: it will stop on an empty cache. Clearing its
	// timer here could race a fired callback and create duplicate sweep loops.
}

func (c *localKeyCache) Range(visit func(any, any) bool) {
	c.mu.Lock()
	items := make([]localKeyItem, 0, len(c.entries))
	for _, e := range c.entries {
		items = append(items, e.Value.(localKeyItem))
	}
	c.mu.Unlock()
	for _, item := range items {
		if !visit(item.key, item.entry) {
			return
		}
	}
}

func (c *localKeyCache) prune() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timer = nil
	now := time.Now()
	for key, e := range c.entries {
		if !now.Before(e.Value.(localKeyItem).entry.expiresAt) {
			delete(c.entries, key)
			c.order.Remove(e)
		}
	}
	if len(c.entries) > 0 {
		c.timer = time.AfterFunc(apiKeyCacheTTL, c.prune)
	}
}

type keyValidationCall struct {
	done chan struct{}
	info *APIKeyInfo
	err  error
}
type keyValidationGroup struct {
	mu         sync.Mutex
	calls      map[string]*keyValidationCall
	maxCalls   int
	second     int64
	lookups    int
	maxLookups int
}

func newKeyValidationGroup(maxCalls, maxLookups int) *keyValidationGroup {
	return &keyValidationGroup{calls: make(map[string]*keyValidationCall), maxCalls: maxCalls, maxLookups: maxLookups}
}

func (g *keyValidationGroup) allowDatabaseLookup() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	second := time.Now().Unix()
	if second != g.second {
		g.second = second
		g.lookups = 0
	}
	if g.lookups >= g.maxLookups {
		return false
	}
	g.lookups++
	return true
}

// One shared completion channel per key, rather than one retained result channel
// per waiter. Canceled waiters leave without accumulating in the shared call.
func (g *keyValidationGroup) do(ctx context.Context, key string, load func(context.Context) (*APIKeyInfo, error)) (*APIKeyInfo, error) {
	g.mu.Lock()
	if call := g.calls[key]; call != nil {
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return call.info, call.err
		}
	}
	if len(g.calls) >= g.maxCalls {
		g.mu.Unlock()
		return nil, ErrAPIKeyLookupBusy
	}
	call := &keyValidationCall{done: make(chan struct{}), err: ErrAPIKeyLookupBusy}
	g.calls[key] = call
	g.mu.Unlock()
	defer func() { g.mu.Lock(); delete(g.calls, key); close(call.done); g.mu.Unlock() }()
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	call.info, call.err = load(loadCtx)
	return call.info, call.err
}

func ValidAPIKeyFormat(key string) bool {
	if len(key) <= 3 || len(key) > 256 || key[:3] != "sk-" {
		return false
	}
	for _, c := range key[3:] {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}

func cachedAPIKey(hash string) (*APIKeyInfo, error, bool) {
	raw, ok := apiKeyCache.Load(hash)
	if !ok {
		return nil, nil, false
	}
	entry := raw.(apiKeyCacheEntry)
	if !time.Now().Before(entry.expiresAt) {
		apiKeyCache.Delete(hash)
		return nil, nil, false
	}
	if entry.info == nil {
		return nil, entry.err, true
	}
	copy := *entry.info
	hydrateAPIKeyInfo(&copy)
	return &copy, entry.err, true
}
