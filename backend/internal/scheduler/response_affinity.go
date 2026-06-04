package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	responseAffinityTTL = 3600 * time.Second

	// responseAffinityMemoryMaxEntries 只是异常流量下的高水位安全阀；正常回收主要靠 TTL。
	responseAffinityMemoryMaxEntries = 1000000
	responseAffinityCleanupInterval  = time.Minute
	responseAffinityCleanupMaxScan   = 4096
	responseAffinityRedisRefreshMin  = time.Minute
)

type responseAffinityBinding struct {
	accountID         int
	expiresAt         time.Time
	redisRefreshAfter time.Time
}

// ResponseAffinity 保存 OpenAI Responses response_id 到上游账号的绑定。
// Redis 存在时用于跨进程粘连；内存缓存作为热缓存和单实例兜底。
type ResponseAffinity struct {
	rdb             *redis.Client
	ttl             time.Duration
	mu              sync.RWMutex
	items           map[string]responseAffinityBinding
	lastCleanupTime time.Time
}

func NewResponseAffinity(rdb *redis.Client) *ResponseAffinity {
	return &ResponseAffinity{
		rdb:             rdb,
		ttl:             responseAffinityTTL,
		items:           make(map[string]responseAffinityBinding, 256),
		lastCleanupTime: time.Now(),
	}
}

func responseAffinityKey(groupID int, platform, responseID string) string {
	return fmt.Sprintf("ag:affinity:response:%d:%s:%s", groupID, strings.TrimSpace(platform), strings.TrimSpace(responseID))
}

func (a *ResponseAffinity) Bind(ctx context.Context, groupID int, platform, responseID string, accountID int) {
	if a == nil || groupID <= 0 || strings.TrimSpace(platform) == "" || strings.TrimSpace(responseID) == "" || accountID <= 0 {
		return
	}
	key := responseAffinityKey(groupID, platform, responseID)
	a.setMemory(key, accountID)
	if a.rdb != nil {
		a.rdb.Set(ctx, key, strconv.Itoa(accountID), a.ttl)
	}
}

func (a *ResponseAffinity) Get(ctx context.Context, groupID int, platform, responseID string) (int, bool) {
	if a == nil || groupID <= 0 || strings.TrimSpace(platform) == "" || strings.TrimSpace(responseID) == "" {
		return 0, false
	}
	key := responseAffinityKey(groupID, platform, responseID)
	if accountID, ok := a.getMemory(key); ok {
		return accountID, true
	}
	if a.rdb == nil {
		return 0, false
	}
	val, err := a.rdb.Get(ctx, key).Result()
	if err != nil {
		return 0, false
	}
	accountID, err := strconv.Atoi(val)
	if err != nil || accountID <= 0 {
		return 0, false
	}
	a.setMemoryRefreshDue(key, accountID)
	return accountID, true
}

func (a *ResponseAffinity) Refresh(ctx context.Context, groupID int, platform, responseID string, accountID int) {
	if a == nil || groupID <= 0 || strings.TrimSpace(platform) == "" || strings.TrimSpace(responseID) == "" || accountID <= 0 {
		return
	}
	key := responseAffinityKey(groupID, platform, responseID)
	shouldRefreshRedis := a.refreshMemory(key, accountID)
	if a.rdb != nil && shouldRefreshRedis {
		a.rdb.Set(ctx, key, strconv.Itoa(accountID), a.ttl)
	}
}

func (a *ResponseAffinity) getMemory(key string) (int, bool) {
	now := time.Now()
	a.mu.RLock()
	binding, ok := a.items[key]
	a.mu.RUnlock()
	if !ok || now.After(binding.expiresAt) || binding.accountID <= 0 {
		if ok {
			a.mu.Lock()
			delete(a.items, key)
			a.mu.Unlock()
		}
		return 0, false
	}
	return binding.accountID, true
}

func (a *ResponseAffinity) setMemory(key string, accountID int) {
	a.setMemoryWithRedisRefreshAfter(key, accountID, time.Now().Add(responseAffinityRedisRefreshMin))
}

func (a *ResponseAffinity) setMemoryRefreshDue(key string, accountID int) {
	a.setMemoryWithRedisRefreshAfter(key, accountID, time.Now())
}

func (a *ResponseAffinity) setMemoryWithRedisRefreshAfter(key string, accountID int, redisRefreshAfter time.Time) {
	if key == "" || accountID <= 0 {
		return
	}
	now := time.Now()
	if redisRefreshAfter.IsZero() {
		redisRefreshAfter = now
	}
	a.cleanupMemory(now)
	a.mu.Lock()
	if len(a.items) >= responseAffinityMemoryMaxEntries {
		deleteOneExpiredOrArbitraryResponseAffinity(a.items, now)
	}
	a.items[key] = responseAffinityBinding{
		accountID:         accountID,
		expiresAt:         now.Add(a.ttl),
		redisRefreshAfter: redisRefreshAfter,
	}
	a.mu.Unlock()
}

func (a *ResponseAffinity) refreshMemory(key string, accountID int) bool {
	if key == "" || accountID <= 0 {
		return false
	}
	now := time.Now()
	a.cleanupMemory(now)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.items) >= responseAffinityMemoryMaxEntries {
		deleteOneExpiredOrArbitraryResponseAffinity(a.items, now)
	}
	binding, ok := a.items[key]
	shouldRefreshRedis := !ok || binding.accountID != accountID || now.After(binding.expiresAt) || !now.Before(binding.redisRefreshAfter)
	if shouldRefreshRedis {
		binding.redisRefreshAfter = now.Add(responseAffinityRedisRefreshMin)
	}
	binding.accountID = accountID
	binding.expiresAt = now.Add(a.ttl)
	a.items[key] = binding
	return shouldRefreshRedis
}

func (a *ResponseAffinity) cleanupMemory(now time.Time) {
	a.mu.RLock()
	shouldCleanup := now.Sub(a.lastCleanupTime) >= responseAffinityCleanupInterval
	a.mu.RUnlock()
	if !shouldCleanup {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Sub(a.lastCleanupTime) < responseAffinityCleanupInterval {
		return
	}
	scanned := 0
	for key, binding := range a.items {
		if now.After(binding.expiresAt) {
			delete(a.items, key)
		}
		scanned++
		if scanned >= responseAffinityCleanupMaxScan {
			break
		}
	}
	a.lastCleanupTime = now
}

func deleteOneExpiredOrArbitraryResponseAffinity(items map[string]responseAffinityBinding, now time.Time) {
	for key, binding := range items {
		if now.After(binding.expiresAt) {
			delete(items, key)
			return
		}
	}
	for key := range items {
		delete(items, key)
		return
	}
}
