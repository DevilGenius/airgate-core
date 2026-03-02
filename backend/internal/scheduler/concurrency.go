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
	// slotTTL 单个请求槽位的过期时间，防止异常未释放
	slotTTL = 5 * time.Minute
)

// ConcurrencyManager 分布式并发槽位管理
// 基于 Redis SET 实现，每个账户一个 SET，成员为 request_id
type ConcurrencyManager struct {
	rdb *redis.Client
}

// NewConcurrencyManager 创建并发管理器
func NewConcurrencyManager(rdb *redis.Client) *ConcurrencyManager {
	return &ConcurrencyManager{rdb: rdb}
}

// concurrencyKey 生成 Redis Key
func concurrencyKey(accountID int) string {
	return fmt.Sprintf("concurrency:%d", accountID)
}

// AcquireSlot 获取并发槽位
// 检查当前 SET 大小 < maxConcurrency，若未满则 SADD
func (cm *ConcurrencyManager) AcquireSlot(ctx context.Context, accountID int, requestID string, maxConcurrency int) error {
	if cm.rdb == nil {
		return nil // Redis 不可用时放行
	}

	key := concurrencyKey(accountID)

	// 使用 Lua 脚本原子性检查并添加
	script := redis.NewScript(`
		local current = redis.call('SCARD', KEYS[1])
		if current < tonumber(ARGV[1]) then
			redis.call('SADD', KEYS[1], ARGV[2])
			redis.call('EXPIRE', KEYS[1], ARGV[3])
			return 1
		end
		return 0
	`)

	result, err := script.Run(ctx, cm.rdb, []string{key},
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

// ReleaseSlot 释放并发槽位
func (cm *ConcurrencyManager) ReleaseSlot(ctx context.Context, accountID int, requestID string) {
	if cm.rdb == nil {
		return
	}

	key := concurrencyKey(accountID)
	cm.rdb.SRem(ctx, key, requestID)
}
