package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// stickyTTL 粘性会话默认过期时间，和 response_id 亲和缓存保持一致。
	stickyTTL              = time.Hour
	stickyMemoryMaxEntries = 65536
	stickyCleanupInterval  = time.Minute
)

type stickyBinding struct {
	accountID int
	expiresAt time.Time
}

// StickySession 粘性会话管理
// 通过 Redis + 进程内缓存保存 session → account 映射，实现对话上下文连续性。
// Redis 不可用或未配置时，进程内缓存仍可提供单实例粘连。
type StickySession struct {
	rdb             *redis.Client
	ttl             time.Duration
	mu              sync.RWMutex
	items           map[string]stickyBinding
	lastCleanupTime time.Time
}

// NewStickySession 创建粘性会话管理器
func NewStickySession(rdb *redis.Client) *StickySession {
	return &StickySession{
		rdb:             rdb,
		ttl:             stickyTTL,
		items:           make(map[string]stickyBinding, 256),
		lastCleanupTime: time.Now(),
	}
}

// stickyKey 生成 Redis Key
// 格式：sticky:{user_id}:{platform}:{session_id}
func stickyKey(userID int, platform, sessionID string) string {
	return fmt.Sprintf("sticky:%d:%s:%s", userID, platform, sessionID)
}

// Get 获取粘性会话绑定的账户 ID
func (s *StickySession) Get(ctx context.Context, userID int, platform, sessionID string) (accountID int, found bool) {
	if s == nil || sessionID == "" {
		return 0, false
	}
	key := stickyKey(userID, platform, sessionID)
	if accountID, found := s.getMemory(key); found {
		return accountID, true
	}

	if s.rdb == nil {
		return 0, false
	}
	val, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		return 0, false
	}

	id, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}
	s.setMemory(key, id)
	return id, true
}

// Set 设置粘性会话绑定（同时续期 TTL）
func (s *StickySession) Set(ctx context.Context, userID int, platform, sessionID string, accountID int) {
	if s == nil || sessionID == "" || accountID <= 0 {
		return
	}
	key := stickyKey(userID, platform, sessionID)
	s.setMemory(key, accountID)
	if s.rdb != nil {
		s.rdb.Set(ctx, key, strconv.Itoa(accountID), s.ttl)
	}
}

func (s *StickySession) getMemory(key string) (int, bool) {
	if key == "" {
		return 0, false
	}
	now := time.Now()
	s.mu.RLock()
	binding, ok := s.items[key]
	s.mu.RUnlock()
	if !ok || now.After(binding.expiresAt) || binding.accountID <= 0 {
		if ok {
			s.mu.Lock()
			delete(s.items, key)
			s.mu.Unlock()
		}
		return 0, false
	}
	return binding.accountID, true
}

func (s *StickySession) setMemory(key string, accountID int) {
	if key == "" || accountID <= 0 {
		return
	}
	s.cleanupMemory(time.Now())
	s.mu.Lock()
	if len(s.items) >= stickyMemoryMaxEntries {
		deleteOneExpiredOrArbitrarySticky(s.items, time.Now())
	}
	s.items[key] = stickyBinding{
		accountID: accountID,
		expiresAt: time.Now().Add(s.ttl),
	}
	s.mu.Unlock()
}

func (s *StickySession) cleanupMemory(now time.Time) {
	s.mu.RLock()
	shouldCleanup := now.Sub(s.lastCleanupTime) >= stickyCleanupInterval
	s.mu.RUnlock()
	if !shouldCleanup {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.Sub(s.lastCleanupTime) < stickyCleanupInterval {
		return
	}
	for key, binding := range s.items {
		if now.After(binding.expiresAt) {
			delete(s.items, key)
		}
	}
	s.lastCleanupTime = now
}

func deleteOneExpiredOrArbitrarySticky(items map[string]stickyBinding, now time.Time) {
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
