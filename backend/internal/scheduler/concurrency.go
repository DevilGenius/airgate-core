package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrConcurrencyLimit = errors.New("并发槽位已满")

const (
	// defaultSlotTTL 单个请求槽位的默认过期时间，防止异常未释放
	defaultSlotTTL = 5 * time.Minute
)

// acquireSlotScript 是 account / apikey / user 三种并发槽共用的原子 Lua 脚本。
//
// 用 ZSET 存储，score = 加入时的 unix 时间戳，member = requestID。
// 同时维护一个短 TTL 的 count key，展示/容量刷新路径用 MGET 读取，避免对账号列表
// 每行执行 ZCOUNT。
// 每次 acquire 前顺手用 ZREMRANGEBYSCORE 把"超过 slotTTL 还没 release 的
// 僵尸 slot" 清理掉——彻底解决因进程 panic / OOM / 重启导致 Release 没跑
// 从而 slot 永远泄漏的历史坑（旧实现用 SET + EXPIRE 整 key，key 的 TTL 又
// 会被后续 acquire 重置，导致只要持续有流量僵尸 slot 就永远清不掉）。
//
// 参数：
//
//	KEYS[1] = 槽位 key
//	KEYS[2] = count key
//	ARGV[1] = 当前 unix 秒
//	ARGV[2] = max_concurrency
//	ARGV[3] = requestID
//	ARGV[4] = slotTTL 秒（既是单个 slot 的存活上限，也是整 key 的兜底 TTL）
//
// 注：三类槽用不同前缀的 key 隔离（ag:concurrency:account:<id> /
// ag:concurrency:apikey:<id> / ag:concurrency:user:<id>），
// 所以同一个脚本可以服务三方而不互相干扰。
var acquireSlotScript = redis.NewScript(`
	local slotKey = KEYS[1]
	local countKey = KEYS[2]
	local now = tonumber(ARGV[1])
	local max = tonumber(ARGV[2])
	local requestID = ARGV[3]
	local ttl = tonumber(ARGV[4])
	local staleBefore = now - ttl

	-- 清理僵尸 slot：score 早于 (now - ttl) 视为泄漏
	redis.call('ZREMRANGEBYSCORE', slotKey, '-inf', staleBefore)

	local current = redis.call('ZCARD', slotKey)
	if current < max then
		redis.call('ZADD', slotKey, now, requestID)
		redis.call('EXPIRE', slotKey, ttl)
		current = redis.call('ZCARD', slotKey)
		redis.call('SET', countKey, current, 'EX', ttl)
		return 1
	end
	if current > 0 then
		redis.call('SET', countKey, current, 'EX', ttl)
	else
		redis.call('DEL', countKey)
	end
	return 0
`)

var releaseSlotScript = redis.NewScript(`
	local slotKey = KEYS[1]
	local countKey = KEYS[2]
	local requestID = ARGV[1]
	local fallbackTTL = tonumber(ARGV[2])

	local removed = redis.call('ZREM', slotKey, requestID)
	local current = redis.call('ZCARD', slotKey)
	if current > 0 then
		local ttl = redis.call('TTL', slotKey)
		if ttl == false or ttl <= 0 then
			ttl = fallbackTTL
		end
		redis.call('SET', countKey, current, 'EX', ttl)
	else
		redis.call('DEL', countKey)
	end
	return removed
`)

// ConcurrencyManager 分布式并发槽位管理。
// 基于 Redis ZSET 实现，每个账户/API Key/用户一个 ZSET，成员为 request_id。
type ConcurrencyManager struct {
	rdb *redis.Client
}

// NewConcurrencyManager 创建并发管理器
func NewConcurrencyManager(rdb *redis.Client) *ConcurrencyManager {
	return &ConcurrencyManager{rdb: rdb}
}

// concurrencyKey 生成账号级 Redis Key。
func concurrencyKey(accountID int) string {
	return fmt.Sprintf("ag:concurrency:account:%d", accountID)
}

func concurrencyCountKey(accountID int) string {
	return fmt.Sprintf("ag:concurrency:account:%d:count", accountID)
}

// apiKeyConcurrencyKey 生成 API Key 级 Redis Key。
func apiKeyConcurrencyKey(keyID int) string {
	return fmt.Sprintf("ag:concurrency:apikey:%d", keyID)
}

func apiKeyConcurrencyCountKey(keyID int) string {
	return fmt.Sprintf("ag:concurrency:apikey:%d:count", keyID)
}

// userConcurrencyKey 生成用户级 Redis Key。
// 用户 A 下的所有 API Key 共享同一个 ZSET，实现"用户总并发"语义。
func userConcurrencyKey(userID int) string {
	return fmt.Sprintf("ag:concurrency:user:%d", userID)
}

func userConcurrencyCountKey(userID int) string {
	return fmt.Sprintf("ag:concurrency:user:%d:count", userID)
}

// acquireSlotByKey 通用并发槽获取：给定 Redis key 和上限，原子性的
// 清理僵尸 slot + 检查上限 + ZADD 加入新 slot（score = 当前时间）。
// maxConcurrency <= 0 时视为不限制，直接放行。
// Redis 不可用时也直接放行，避免影响主链路可用性。
func (cm *ConcurrencyManager) acquireSlotByKey(ctx context.Context, key, countKey, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	if cm.rdb == nil || maxConcurrency <= 0 {
		return nil
	}
	if slotTTL <= 0 {
		slotTTL = defaultSlotTTL
	}

	now := time.Now().Unix()
	result, err := acquireSlotScript.Run(ctx, cm.rdb, []string{key, countKey},
		now,
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

func (cm *ConcurrencyManager) releaseSlotByKey(ctx context.Context, key, countKey, requestID string) {
	if cm.rdb == nil {
		return
	}
	_, _ = releaseSlotScript.Run(ctx, cm.rdb, []string{key, countKey}, requestID, int(defaultSlotTTL.Seconds())).Result()
}

// AcquireSlot 获取账号级并发槽位。
// 检查当前 SET 大小 < maxConcurrency，若未满则 SADD。
// slotTTL 为槽位过期时间，<= 0 时使用默认值（5 分钟）。
func (cm *ConcurrencyManager) AcquireSlot(ctx context.Context, accountID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	return cm.acquireSlotByKey(ctx, concurrencyKey(accountID), concurrencyCountKey(accountID), requestID, maxConcurrency, slotTTL)
}

// ReleaseSlot 释放账号级并发槽位
func (cm *ConcurrencyManager) ReleaseSlot(ctx context.Context, accountID int, requestID string) {
	cm.releaseSlotByKey(ctx, concurrencyKey(accountID), concurrencyCountKey(accountID), requestID)
}

// AcquireAPIKeySlot 获取 API Key 级并发槽位。
// maxConcurrency <= 0 时直接放行（表示该 key 不限制并发）。
// 与账号级并发独立，两层闸门各自计数，调用方需要分别 release。
func (cm *ConcurrencyManager) AcquireAPIKeySlot(ctx context.Context, keyID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	return cm.acquireSlotByKey(ctx, apiKeyConcurrencyKey(keyID), apiKeyConcurrencyCountKey(keyID), requestID, maxConcurrency, slotTTL)
}

// ReleaseAPIKeySlot 释放 API Key 级并发槽位
func (cm *ConcurrencyManager) ReleaseAPIKeySlot(ctx context.Context, keyID int, requestID string) {
	cm.releaseSlotByKey(ctx, apiKeyConcurrencyKey(keyID), apiKeyConcurrencyCountKey(keyID), requestID)
}

// AcquireUserSlot 获取用户级并发槽位。
// maxConcurrency <= 0 时直接放行（表示该用户不限制总并发）。
// 与 apikey / 账号 两级槽位独立，调用方需要分别 release。
func (cm *ConcurrencyManager) AcquireUserSlot(ctx context.Context, userID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	return cm.acquireSlotByKey(ctx, userConcurrencyKey(userID), userConcurrencyCountKey(userID), requestID, maxConcurrency, slotTTL)
}

// ReleaseUserSlot 释放用户级并发槽位
func (cm *ConcurrencyManager) ReleaseUserSlot(ctx context.Context, userID int, requestID string) {
	cm.releaseSlotByKey(ctx, userConcurrencyKey(userID), userConcurrencyCountKey(userID), requestID)
}

// GetCurrentCount 获取账户当前并发数。
// 读 acquire/release 写入的短 TTL count key，避免展示路径执行 ZCOUNT。
func (cm *ConcurrencyManager) GetCurrentCount(ctx context.Context, accountID int) int {
	if cm.rdb == nil {
		return 0
	}
	n, err := cm.rdb.Get(ctx, concurrencyCountKey(accountID)).Int()
	if err != nil {
		return 0
	}
	return n
}

// GetCurrentCounts 批量获取多个账户的当前并发数。
// 容量刷新只做一次 MGET；不能按账号执行 ZCOUNT，否则 100 行页面 0.5s 刷新会放大成
// 200 次/s Redis 命令。
func (cm *ConcurrencyManager) GetCurrentCounts(ctx context.Context, accountIDs []int) map[int]int {
	result := make(map[int]int, len(accountIDs))
	if cm.rdb == nil || len(accountIDs) == 0 {
		return result
	}
	keys := make([]string, 0, len(accountIDs))
	for _, id := range accountIDs {
		keys = append(keys, concurrencyCountKey(id))
	}
	values, err := cm.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return result
	}
	for index, value := range values {
		if value == nil {
			continue
		}
		raw, ok := value.(string)
		if !ok {
			continue
		}
		count, err := strconv.Atoi(raw)
		if err == nil && count > 0 {
			result[accountIDs[index]] = count
		}
	}
	return result
}
