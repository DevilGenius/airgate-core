package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrConcurrencyLimit = errors.New("并发槽位已满")

const (
	// defaultSlotTTL 单个请求槽位的默认过期时间，防止异常未释放
	defaultSlotTTL = 5 * time.Minute
)

// acquireSlotScript 是 account / apikey 两种并发槽共用的原子 Lua 脚本。
// 逻辑：SCARD < max → SADD 并续期；否则返回 0。
// 注：两类槽用不同前缀的 key 隔离（concurrency:<id> 与 concurrency:apikey:<id>），
// 所以同一个脚本可以服务双方而不互相干扰。
var acquireSlotScript = redis.NewScript(`
	local current = redis.call('SCARD', KEYS[1])
	if current < tonumber(ARGV[1]) then
		redis.call('SADD', KEYS[1], ARGV[2])
		redis.call('EXPIRE', KEYS[1], ARGV[3])
		return 1
	end
	return 0
`)

// ConcurrencyManager 分布式并发槽位管理
// 基于 Redis SET 实现，每个账户一个 SET，成员为 request_id
type ConcurrencyManager struct {
	rdb *redis.Client
}

// NewConcurrencyManager 创建并发管理器
func NewConcurrencyManager(rdb *redis.Client) *ConcurrencyManager {
	return &ConcurrencyManager{rdb: rdb}
}

// concurrencyKey 生成账号级 Redis Key
func concurrencyKey(accountID int) string {
	return fmt.Sprintf("concurrency:%d", accountID)
}

// apiKeyConcurrencyKey 生成 API Key 级 Redis Key。
// 与账号级 key 用不同前缀隔离，避免 key_id / account_id 数值相同时相互串扰。
func apiKeyConcurrencyKey(keyID int) string {
	return fmt.Sprintf("concurrency:apikey:%d", keyID)
}

// acquireSlotByKey 通用并发槽获取：给定 Redis key 和上限，原子性检查 + SADD。
// maxConcurrency <= 0 时视为不限制，直接放行。
// Redis 不可用时也直接放行，避免影响主链路可用性。
func (cm *ConcurrencyManager) acquireSlotByKey(ctx context.Context, key, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	if cm.rdb == nil || maxConcurrency <= 0 {
		return nil
	}
	if slotTTL <= 0 {
		slotTTL = defaultSlotTTL
	}

	result, err := acquireSlotScript.Run(ctx, cm.rdb, []string{key},
		maxConcurrency,
		requestID,
		int(slotTTL.Seconds()),
	).Int()

	if err != nil {
		// Redis 不可用时放行
		return nil
	}

	if result == 0 {
		return ErrConcurrencyLimit
	}
	return nil
}

// AcquireSlot 获取账号级并发槽位。
// 检查当前 SET 大小 < maxConcurrency，若未满则 SADD。
// slotTTL 为槽位过期时间，<= 0 时使用默认值（5 分钟）。
func (cm *ConcurrencyManager) AcquireSlot(ctx context.Context, accountID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	return cm.acquireSlotByKey(ctx, concurrencyKey(accountID), requestID, maxConcurrency, slotTTL)
}

// ReleaseSlot 释放账号级并发槽位
func (cm *ConcurrencyManager) ReleaseSlot(ctx context.Context, accountID int, requestID string) {
	if cm.rdb == nil {
		return
	}

	key := concurrencyKey(accountID)
	cm.rdb.SRem(ctx, key, requestID)
}

// AcquireAPIKeySlot 获取 API Key 级并发槽位。
// maxConcurrency <= 0 时直接放行（表示该 key 不限制并发）。
// 与账号级并发独立，两层闸门各自计数，调用方需要分别 release。
func (cm *ConcurrencyManager) AcquireAPIKeySlot(ctx context.Context, keyID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	return cm.acquireSlotByKey(ctx, apiKeyConcurrencyKey(keyID), requestID, maxConcurrency, slotTTL)
}

// ReleaseAPIKeySlot 释放 API Key 级并发槽位
func (cm *ConcurrencyManager) ReleaseAPIKeySlot(ctx context.Context, keyID int, requestID string) {
	if cm.rdb == nil {
		return
	}
	cm.rdb.SRem(ctx, apiKeyConcurrencyKey(keyID), requestID)
}

// GetCurrentCount 获取账户当前并发数
func (cm *ConcurrencyManager) GetCurrentCount(ctx context.Context, accountID int) int {
	if cm.rdb == nil {
		return 0
	}
	n, err := cm.rdb.SCard(ctx, concurrencyKey(accountID)).Result()
	if err != nil {
		return 0
	}
	return int(n)
}

// GetCurrentCounts 批量获取多个账户的当前并发数
func (cm *ConcurrencyManager) GetCurrentCounts(ctx context.Context, accountIDs []int) map[int]int {
	result := make(map[int]int, len(accountIDs))
	if cm.rdb == nil {
		return result
	}
	pipe := cm.rdb.Pipeline()
	cmds := make(map[int]*redis.IntCmd, len(accountIDs))
	for _, id := range accountIDs {
		cmds[id] = pipe.SCard(ctx, concurrencyKey(id))
	}
	_, _ = pipe.Exec(ctx)
	for id, cmd := range cmds {
		if n, err := cmd.Result(); err == nil {
			result[id] = int(n)
		}
	}
	return result
}
