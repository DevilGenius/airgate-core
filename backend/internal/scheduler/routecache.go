package scheduler

import (
	"sync"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
)

// Route 结果缓存。
//
// routeAccounts(groupID, platform) 在高并发下会被反复调用（每次请求一次）——
// 其中 group.Get + group.QueryAccounts 是两次 DB 往返，账号多时 QueryAccounts
// 还会拉回全量账户行。5000 并发 × 10+ 次 failover 最坏能打出 5 万次 DB 查询，
// 连接池再大也扛不住。
//
// 缓存的是"路由结果"——分组下匹配 platform + model_routing 的账号列表。这些
// 字段只有在 admin 改分组 / 增删账号时才会变，正常业务流量不会触发。
//
// 账号 state / state_until / max_concurrency / extra 随调用一起被快照：
//   - state: 3s 内可能陈旧，但 checkSchedulability 会过滤，最多浪费一次 failover
//   - max_concurrency / extra / last_used_at：3s 内的偏差不影响调度正确性
//
// 关键转移（Active ↔ Disabled）由 state machine 主动调用 InvalidateAll，而
// RateLimited / Degraded 这种带 state_until 的转移由 TTL 兜底，不另外 invalidate。
const (
	routeCacheTTL             = 3 * time.Second
	routeCacheMaxEntries      = 262144
	routeCacheCleanupInterval = time.Minute
)

type routeCacheKey struct {
	groupID  int
	platform string
}

type routeCacheEntry struct {
	accounts     []*ent.Account
	modelRouting map[string][]int64
	expiresAt    time.Time
}

// routeCache 路由结果缓存。并发安全，所有 method 都是 O(1)。
type routeCache struct {
	ttl             time.Duration
	mu              sync.RWMutex
	store           map[routeCacheKey]routeCacheEntry
	lastCleanupTime time.Time
}

func newRouteCache(ttl time.Duration) *routeCache {
	return &routeCache{
		ttl:             ttl,
		store:           make(map[routeCacheKey]routeCacheEntry),
		lastCleanupTime: time.Now(),
	}
}

// Get 返回命中的账号列表 + model routing（若未过期）。未命中或过期返回 ok=false。
func (c *routeCache) Get(groupID int, platform string) ([]*ent.Account, map[string][]int64, bool) {
	if c == nil {
		return nil, nil, false
	}
	key := routeCacheKey{groupID, platform}
	now := time.Now()
	c.mu.RLock()
	e, ok := c.store[key]
	c.mu.RUnlock()
	if !ok {
		return nil, nil, false
	}
	if now.After(e.expiresAt) {
		c.deleteExpired(key, e.expiresAt, now)
		return nil, nil, false
	}
	return e.accounts, e.modelRouting, true
}

// Set 写入一条缓存。
func (c *routeCache) Set(groupID int, platform string, accounts []*ent.Account, routing map[string][]int64) {
	if c == nil {
		return
	}
	now := time.Now()
	key := routeCacheKey{groupID, platform}
	c.mu.Lock()
	c.cleanupExpiredLocked(now)
	if _, exists := c.store[key]; !exists && len(c.store) >= routeCacheMaxEntries {
		c.deleteOneExpiredOrArbitraryLocked(now)
	}
	c.store[key] = routeCacheEntry{
		accounts:     accounts,
		modelRouting: routing,
		expiresAt:    now.Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *routeCache) deleteExpired(key routeCacheKey, expiresAt time.Time, now time.Time) {
	c.mu.Lock()
	if current, ok := c.store[key]; ok && current.expiresAt.Equal(expiresAt) && now.After(current.expiresAt) {
		delete(c.store, key)
	}
	c.mu.Unlock()
}

func (c *routeCache) cleanupExpiredLocked(now time.Time) {
	if now.Sub(c.lastCleanupTime) < routeCacheCleanupInterval && len(c.store) < routeCacheMaxEntries {
		return
	}
	for key, entry := range c.store {
		if now.After(entry.expiresAt) {
			delete(c.store, key)
		}
	}
	c.lastCleanupTime = now
}

func (c *routeCache) deleteOneExpiredOrArbitraryLocked(now time.Time) {
	for key, entry := range c.store {
		if now.After(entry.expiresAt) {
			delete(c.store, key)
			return
		}
	}
	for key := range c.store {
		delete(c.store, key)
		return
	}
}

// InvalidateGroup 清除指定分组的所有 platform 缓存。admin 更新分组 / 账号绑定时调用。
func (c *routeCache) InvalidateGroup(groupID int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	for k := range c.store {
		if k.groupID == groupID {
			delete(c.store, k)
		}
	}
	c.mu.Unlock()
}

// InvalidateAll 清空所有缓存。状态机在 Active ↔ Disabled 转移时调用。
func (c *routeCache) InvalidateAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.store = make(map[routeCacheKey]routeCacheEntry)
	c.lastCleanupTime = time.Now()
	c.mu.Unlock()
}
