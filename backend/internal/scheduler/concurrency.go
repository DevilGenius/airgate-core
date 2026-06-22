package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrConcurrencyLimit = errors.New("并发槽位已满")

const (
	// defaultSlotTTL 单个请求槽位的默认过期时间，防止异常未释放
	defaultSlotTTL = 5 * time.Minute
	// concurrencyZeroCountTTL 缓存空槽位读数，避免容量刷新对空账号反复回落到 ZCARD。
	concurrencyZeroCountTTL = 30 * time.Second
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
//	KEYS[3] = 可选：账号工作中索引 key（仅账号级并发）
//	ARGV[1] = 当前 unix 秒
//	ARGV[2] = max_concurrency
//	ARGV[3] = requestID
//	ARGV[4] = slotTTL 秒（既是单个 slot 的存活上限，也是整 key 的兜底 TTL）
//	ARGV[5] = 可选：账号 ID（仅账号级并发）
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
	local indexKey = KEYS[3]
	local accountID = ARGV[5]
	local staleBefore = now - ttl

	-- 清理僵尸 slot：score 早于 (now - ttl) 视为泄漏
	local staleRemoved = redis.call('ZREMRANGEBYSCORE', slotKey, '-inf', staleBefore)

	local current = redis.call('ZCARD', slotKey)
	if current < max then
		redis.call('ZADD', slotKey, now, requestID)
		redis.call('EXPIRE', slotKey, ttl)
		current = redis.call('ZCARD', slotKey)
		redis.call('SET', countKey, current, 'EX', ttl)
		if indexKey and accountID then
			redis.call('ZADD', indexKey, current, accountID)
		end
		return {1, current}
	end
	if current > 0 then
		redis.call('SET', countKey, current, 'EX', ttl)
		if staleRemoved > 0 and indexKey and accountID then
			redis.call('ZADD', indexKey, current, accountID)
		end
	else
		redis.call('DEL', countKey)
		if indexKey and accountID then
			redis.call('ZREM', indexKey, accountID)
		end
	end
	return {0, current}
`)

var releaseSlotScript = redis.NewScript(`
	local slotKey = KEYS[1]
	local countKey = KEYS[2]
	local indexKey = KEYS[3]
	local requestID = ARGV[1]
	local fallbackTTL = tonumber(ARGV[2])
	local zeroTTL = tonumber(ARGV[3])
	local accountID = ARGV[4]

	local removed = redis.call('ZREM', slotKey, requestID)
	local current = redis.call('ZCARD', slotKey)
	if current > 0 then
		local ttl = redis.call('TTL', slotKey)
		if ttl == false or ttl <= 0 then
			ttl = fallbackTTL
		end
		redis.call('SET', countKey, current, 'EX', ttl)
		if removed > 0 and indexKey and accountID then
			redis.call('ZADD', indexKey, current, accountID)
		end
	else
		redis.call('SET', countKey, 0, 'EX', zeroTTL)
		if indexKey and accountID then
			redis.call('ZREM', indexKey, accountID)
		end
	end
	return {removed, current}
`)

var backfillConcurrencyCountsScript = redis.NewScript(`
	local now = tonumber(ARGV[1])
	local slotTTL = tonumber(ARGV[2])
	local zeroTTL = tonumber(ARGV[3])
	local staleBefore = now - slotTTL
	local out = {}

	for index = 1, #KEYS, 2 do
		local slotKey = KEYS[index]
		local countKey = KEYS[index + 1]
		redis.call('ZREMRANGEBYSCORE', slotKey, '-inf', staleBefore)
		local current = redis.call('ZCARD', slotKey)
		if current > 0 then
			local ttl = redis.call('TTL', slotKey)
			if ttl == false or ttl <= 0 then
				ttl = slotTTL
			end
			redis.call('SET', countKey, current, 'EX', ttl)
		else
			redis.call('SET', countKey, 0, 'EX', zeroTTL)
		end
		table.insert(out, current)
	end

	return out
`)

// ConcurrencyManager 分布式并发槽位管理。
// 基于 Redis ZSET 实现，每个账户/API Key/用户一个 ZSET，成员为 request_id。
type ConcurrencyManager struct {
	rdb               *redis.Client
	capacityPublisher CapacityEventPublisher
}

// NewConcurrencyManager 创建并发管理器
func NewConcurrencyManager(rdb *redis.Client) *ConcurrencyManager {
	return &ConcurrencyManager{rdb: rdb}
}

// CapacityEventPublisher receives best-effort account capacity changes.
type CapacityEventPublisher interface {
	PublishAccountCapacityChanged(accountID int, currentConcurrency int)
}

// SetCapacityEventPublisher injects a best-effort capacity event publisher.
func (cm *ConcurrencyManager) SetCapacityEventPublisher(publisher CapacityEventPublisher) {
	if cm == nil {
		return
	}
	cm.capacityPublisher = publisher
}

// concurrencyKey 生成账号级 Redis Key。
func concurrencyKey(accountID int) string {
	return fmt.Sprintf("ag:concurrency:account:%d", accountID)
}

func concurrencyCountKey(accountID int) string {
	return fmt.Sprintf("ag:concurrency:account:%d:count", accountID)
}

func accountConcurrencyWorkingIndexKey() string {
	return "ag:concurrency:account:working"
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
func (cm *ConcurrencyManager) acquireSlotByKey(ctx context.Context, key, countKey, indexKey, indexMember, requestID string, maxConcurrency int, slotTTL time.Duration) (int, bool, error) {
	if cm.rdb == nil || maxConcurrency <= 0 {
		return 0, false, nil
	}
	if slotTTL <= 0 {
		slotTTL = defaultSlotTTL
	}

	now := time.Now().Unix()
	keys := []string{key, countKey}
	args := []interface{}{
		now,
		maxConcurrency,
		requestID,
		int(slotTTL.Seconds()),
	}
	if indexKey != "" && indexMember != "" {
		keys = append(keys, indexKey)
		args = append(args, indexMember)
	}
	raw, err := acquireSlotScript.Run(ctx, cm.rdb, keys, args...).Result()

	if err != nil {
		// Redis 不可用时放行
		return 0, false, nil
	}
	result, current, ok := parseSlotScriptResult(raw)
	if !ok {
		return 0, false, nil
	}

	if result == 0 {
		return current, false, ErrConcurrencyLimit
	}
	return current, true, nil
}

func (cm *ConcurrencyManager) releaseSlotByKey(ctx context.Context, key, countKey, indexKey, indexMember, requestID string) (int, bool) {
	if cm.rdb == nil {
		return 0, false
	}
	keys := []string{key, countKey}
	args := []interface{}{
		requestID,
		int(defaultSlotTTL.Seconds()),
		int(concurrencyZeroCountTTL.Seconds()),
	}
	if indexKey != "" && indexMember != "" {
		keys = append(keys, indexKey)
		args = append(args, indexMember)
	}
	raw, err := releaseSlotScript.Run(ctx, cm.rdb, keys, args...).Result()
	if err != nil {
		return 0, false
	}
	removed, current, ok := parseSlotScriptResult(raw)
	return current, ok && removed > 0
}

// AcquireSlot 获取账号级并发槽位。
// 检查当前 SET 大小 < maxConcurrency，若未满则 SADD。
// slotTTL 为槽位过期时间，<= 0 时使用默认值（5 分钟）。
func (cm *ConcurrencyManager) AcquireSlot(ctx context.Context, accountID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	accountIDString := strconv.Itoa(accountID)
	current, changed, err := cm.acquireSlotByKey(ctx, concurrencyKey(accountID), concurrencyCountKey(accountID), accountConcurrencyWorkingIndexKey(), accountIDString, requestID, maxConcurrency, slotTTL)
	if err == nil && changed {
		cm.publishAccountCapacity(accountID, current)
	}
	return err
}

// ReleaseSlot 释放账号级并发槽位
func (cm *ConcurrencyManager) ReleaseSlot(ctx context.Context, accountID int, requestID string) {
	accountIDString := strconv.Itoa(accountID)
	current, changed := cm.releaseSlotByKey(ctx, concurrencyKey(accountID), concurrencyCountKey(accountID), accountConcurrencyWorkingIndexKey(), accountIDString, requestID)
	if changed {
		cm.publishAccountCapacity(accountID, current)
	}
}

// AcquireAPIKeySlot 获取 API Key 级并发槽位。
// maxConcurrency <= 0 时直接放行（表示该 key 不限制并发）。
// 与账号级并发独立，两层闸门各自计数，调用方需要分别 release。
func (cm *ConcurrencyManager) AcquireAPIKeySlot(ctx context.Context, keyID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	_, _, err := cm.acquireSlotByKey(ctx, apiKeyConcurrencyKey(keyID), apiKeyConcurrencyCountKey(keyID), "", "", requestID, maxConcurrency, slotTTL)
	return err
}

// ReleaseAPIKeySlot 释放 API Key 级并发槽位
func (cm *ConcurrencyManager) ReleaseAPIKeySlot(ctx context.Context, keyID int, requestID string) {
	cm.releaseSlotByKey(ctx, apiKeyConcurrencyKey(keyID), apiKeyConcurrencyCountKey(keyID), "", "", requestID)
}

// AcquireUserSlot 获取用户级并发槽位。
// maxConcurrency <= 0 时直接放行（表示该用户不限制总并发）。
// 与 apikey / 账号 两级槽位独立，调用方需要分别 release。
func (cm *ConcurrencyManager) AcquireUserSlot(ctx context.Context, userID int, requestID string, maxConcurrency int, slotTTL time.Duration) error {
	_, _, err := cm.acquireSlotByKey(ctx, userConcurrencyKey(userID), userConcurrencyCountKey(userID), "", "", requestID, maxConcurrency, slotTTL)
	return err
}

// ReleaseUserSlot 释放用户级并发槽位
func (cm *ConcurrencyManager) ReleaseUserSlot(ctx context.Context, userID int, requestID string) {
	cm.releaseSlotByKey(ctx, userConcurrencyKey(userID), userConcurrencyCountKey(userID), "", "", requestID)
}

func (cm *ConcurrencyManager) publishAccountCapacity(accountID int, current int) {
	if cm == nil || cm.capacityPublisher == nil || accountID <= 0 {
		return
	}
	if current < 0 {
		current = 0
	}
	cm.capacityPublisher.PublishAccountCapacityChanged(accountID, current)
}

func parseSlotScriptResult(raw any) (int, int, bool) {
	values, ok := raw.([]interface{})
	if !ok || len(values) < 2 {
		return 0, 0, false
	}
	changed, ok := redisIntValue(values[0])
	if !ok {
		return 0, 0, false
	}
	current, ok := redisIntValue(values[1])
	if !ok {
		return 0, 0, false
	}
	return changed, current, true
}

// GetCurrentCount 获取账户当前并发数。
// 读 acquire/release 写入的短 TTL count key，避免展示路径执行 ZCOUNT。
func (cm *ConcurrencyManager) GetCurrentCount(ctx context.Context, accountID int) int {
	if cm.rdb == nil {
		return 0
	}
	counts := loadConcurrencyCounts(ctx, cm.rdb, []int{accountID}, true)
	return counts[accountID]
}

func loadConcurrencyCounts(ctx context.Context, rdb *redis.Client, accountIDs []int, backfillMissing bool) map[int]int {
	result := make(map[int]int, len(accountIDs))
	if rdb == nil || len(accountIDs) == 0 {
		return result
	}
	ids := uniqueAccountIDs(accountIDs)
	if len(ids) == 0 {
		return result
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, concurrencyCountKey(id))
	}
	values, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return result
	}
	missingIDs := make([]int, 0)
	for index, value := range values {
		count, ok := redisIntValue(value)
		if !ok {
			missingIDs = append(missingIDs, ids[index])
			continue
		}
		if count > 0 {
			result[ids[index]] = count
		}
	}
	if backfillMissing && len(missingIDs) > 0 {
		backfillConcurrencyCounts(ctx, rdb, missingIDs, result)
	}
	return result
}

func backfillConcurrencyCounts(ctx context.Context, rdb *redis.Client, accountIDs []int, result map[int]int) {
	if rdb == nil || len(accountIDs) == 0 {
		return
	}
	keys := make([]string, 0, len(accountIDs)*2)
	for _, id := range accountIDs {
		keys = append(keys, concurrencyKey(id), concurrencyCountKey(id))
	}
	raw, err := backfillConcurrencyCountsScript.Run(ctx, rdb, keys,
		time.Now().Unix(),
		int(defaultSlotTTL.Seconds()),
		int(concurrencyZeroCountTTL.Seconds()),
	).Result()
	if err != nil {
		return
	}
	values, ok := raw.([]interface{})
	if !ok {
		return
	}
	for index, value := range values {
		if index >= len(accountIDs) {
			break
		}
		count, ok := redisIntValue(value)
		if ok && count > 0 {
			result[accountIDs[index]] = count
		}
	}
}

// GetCurrentCounts 批量获取多个账户的当前并发数。
// 容量刷新只做一次 MGET；miss 视为 0。acquire/release 会维护非零 count key，
// 展示路径不能为确认空账号回落到每账号 ZSET 清理/ZCARD。
func (cm *ConcurrencyManager) GetCurrentCounts(ctx context.Context, accountIDs []int) map[int]int {
	return loadConcurrencyCounts(ctx, cm.rdb, accountIDs, false)
}

// GetWorkingCounts returns account IDs whose current account-level concurrency is > 0.
func (cm *ConcurrencyManager) GetWorkingCounts(ctx context.Context) map[int]int {
	if cm == nil || cm.rdb == nil {
		return map[int]int{}
	}
	return loadWorkingConcurrencyCounts(ctx, cm.rdb)
}

func loadWorkingConcurrencyCounts(ctx context.Context, rdb *redis.Client) map[int]int {
	result := make(map[int]int)
	if rdb == nil {
		return result
	}

	members, err := rdb.ZRange(ctx, accountConcurrencyWorkingIndexKey(), 0, -1).Result()
	if err == nil && len(members) > 0 {
		ids := accountIDsFromRedisMembers(members)
		counts := loadConcurrencyCounts(ctx, rdb, ids, true)
		if len(counts) < len(ids) {
			removeStaleWorkingIndexMembers(ctx, rdb, members, counts)
		}
		if len(counts) > 0 {
			return counts
		}
	}

	return scanWorkingConcurrencyCounts(ctx, rdb)
}

func accountIDsFromRedisMembers(members []string) []int {
	ids := make([]int, 0, len(members))
	seen := make(map[int]struct{}, len(members))
	for _, member := range members {
		id, err := strconv.Atoi(member)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func removeWorkingIndexMembers(ctx context.Context, rdb *redis.Client, members []string) {
	if len(members) == 0 {
		return
	}
	args := make([]interface{}, 0, len(members))
	for _, member := range members {
		args = append(args, member)
	}
	_ = rdb.ZRem(ctx, accountConcurrencyWorkingIndexKey(), args...).Err()
}

func removeStaleWorkingIndexMembers(ctx context.Context, rdb *redis.Client, members []string, counts map[int]int) {
	stale := make([]string, 0)
	for _, member := range members {
		id, err := strconv.Atoi(member)
		if err != nil || id <= 0 {
			stale = append(stale, member)
			continue
		}
		if counts[id] <= 0 {
			stale = append(stale, member)
		}
	}
	removeWorkingIndexMembers(ctx, rdb, stale)
}

func scanWorkingConcurrencyCounts(ctx context.Context, rdb *redis.Client) map[int]int {
	result := make(map[int]int)
	var cursor uint64
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, "ag:concurrency:account:*:count", 1000).Result()
		if err != nil {
			return result
		}
		addWorkingCountsFromKeys(ctx, rdb, keys, result)
		cursor = nextCursor
		if cursor == 0 {
			return result
		}
	}
}

func addWorkingCountsFromKeys(ctx context.Context, rdb *redis.Client, keys []string, result map[int]int) {
	if len(keys) == 0 {
		return
	}
	ids := make([]int, 0, len(keys))
	countKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		id, ok := accountIDFromConcurrencyCountKey(key)
		if !ok {
			continue
		}
		ids = append(ids, id)
		countKeys = append(countKeys, key)
	}
	if len(countKeys) == 0 {
		return
	}
	values, err := rdb.MGet(ctx, countKeys...).Result()
	if err != nil {
		return
	}
	pipe := rdb.Pipeline()
	indexWrites := 0
	for index, value := range values {
		count, ok := redisIntValue(value)
		if !ok || count <= 0 {
			continue
		}
		id := ids[index]
		result[id] = count
		pipe.ZAdd(ctx, accountConcurrencyWorkingIndexKey(), redis.Z{
			Score:  float64(count),
			Member: strconv.Itoa(id),
		})
		indexWrites++
	}
	if indexWrites > 0 {
		_, _ = pipe.Exec(ctx)
	}
}

func accountIDFromConcurrencyCountKey(key string) (int, bool) {
	const prefix = "ag:concurrency:account:"
	const suffix = ":count"
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return 0, false
	}
	rawID := strings.TrimSuffix(strings.TrimPrefix(key, prefix), suffix)
	id, err := strconv.Atoi(rawID)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
