package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ModelFamily 把 (platform, model) 折叠成上游限流共享的"家族"键。
//
// 用于把"账号-家族"维度的限流冷却隔离开 —— 例如 gpt-image 撞 4000/min
// 不应该影响同账号上 chat 模型的可用性。OpenAI 的限流维度大多是 per-model 或
// per-family（同系列共享一个池），所以把限流冷却按家族打而不是按账号，更贴近上游真实行为。
//
// 当前规则：
//   - openai 平台下，gpt-image-* 系列共享 "gpt-image"
//   - 其它情况：直接用 model 本身作为家族键（每个 model 独立冷却）
//   - model 为空：用 platform 兜底，保持后向兼容
//
// 后续若发现有更多上游限流共享组（例如 gpt-5 家族共享 IPM），在此扩展即可。
func ModelFamily(platform, model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(m, "gpt-image") {
		return "gpt-image"
	}
	if m != "" {
		return m
	}
	return strings.ToLower(strings.TrimSpace(platform))
}

// FamilyCooldown 维护"账号 × 模型家族"的限流冷却，落 Redis、按 TTL 自然恢复。
//
// 与 DB 上的 Account.State 区别：
//   - DB state（rate_limited / disabled / degraded）是账号级，影响整账号所有调用。
//   - FamilyCooldown 是 (account, family) 级；撞 gpt-image 不会让 chat 流量受牵连。
//
// 短时高频（200ms~60s）的限流非常适合放 Redis：写读都廉价、过期由 TTL 兜底，
// 重启即清不影响业务（重启后再撞一次 429 又会重新写入）。
//
// Redis 不可用时退化为"不冷却"（fail-open），保证主链路可用性。
type FamilyCooldown struct {
	rdb *redis.Client
}

const (
	familyCooldownIndexTTL   = 24 * time.Hour
	familyCooldownIndexGrace = time.Minute
)

const familyCooldownIndexExpireScript = `
local ttl = redis.call("PTTL", KEYS[1])
local next_ttl = tonumber(ARGV[1])
if ttl < next_ttl then
	redis.call("PEXPIRE", KEYS[1], next_ttl)
end
return 1
`

// NewFamilyCooldown 构造家族冷却管理器。rdb=nil 时所有方法 no-op。
func NewFamilyCooldown(rdb *redis.Client) *FamilyCooldown {
	return &FamilyCooldown{rdb: rdb}
}

// familyCooldownIndexKey 保存账号当前冷却家族的过期时间索引。
func familyCooldownIndexKey(accountID int) string {
	return fmt.Sprintf("ag:cooldown:family:%d:index", accountID)
}

// familyCooldownReasonKey 保存账号和模型家族维度的冷却原因。
func familyCooldownReasonKey(accountID int, family string) string {
	return fmt.Sprintf("ag:cooldown:family:%d:reason:%s", accountID, family)
}

// Mark 把 (account, family) 写入冷却，TTL = until - now（最少 1ms）。
// 旧的 cooldown 直接被覆盖：上游每次给的 Retry-After 都视为最新建议，无须保留历史。
func (fc *FamilyCooldown) Mark(ctx context.Context, accountID int, family string, until time.Time, reason string) {
	if fc == nil || fc.rdb == nil || family == "" {
		return
	}
	ttl := time.Until(until)
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	indexTTL := ttl + familyCooldownIndexGrace
	if indexTTL < familyCooldownIndexTTL {
		indexTTL = familyCooldownIndexTTL
	}
	indexKey := familyCooldownIndexKey(accountID)
	pipe := fc.rdb.Pipeline()
	pipe.Set(ctx, familyCooldownReasonKey(accountID, family), reason, ttl)
	pipe.ZAdd(ctx, indexKey, redis.Z{Score: float64(until.UnixMilli()), Member: family})
	pipe.Eval(ctx, familyCooldownIndexExpireScript, []string{indexKey}, indexTTL.Milliseconds())
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Debug("写入家族冷却失败",
			"account_id", accountID, "family", family, "ttl_ms", ttl.Milliseconds(), "error", err)
	}
}

// Until 查询 (account, family) 的冷却到期时间。
// 没有冷却返回 (zero, false)；Redis 不可用时也返回 (zero, false) —— 失败开放，
// 宁可让一次请求撞墙，也不能因为 Redis 抖动让整池账号不可用。
func (fc *FamilyCooldown) Until(ctx context.Context, accountID int, family string) (time.Time, bool) {
	if fc == nil || fc.rdb == nil || family == "" {
		return time.Time{}, false
	}
	ttl, err := fc.rdb.TTL(ctx, familyCooldownReasonKey(accountID, family)).Result()
	if err != nil || ttl <= 0 {
		return time.Time{}, false
	}
	return time.Now().Add(ttl), true
}

// Clear 清除指定家族的冷却。管理员强制解封 / 测试场景使用。
// 业务正常路径不需要主动清，TTL 到期自动清掉。
func (fc *FamilyCooldown) Clear(ctx context.Context, accountID int, family string) {
	if fc == nil || fc.rdb == nil || family == "" {
		return
	}
	pipe := fc.rdb.Pipeline()
	pipe.Del(ctx, familyCooldownReasonKey(accountID, family))
	pipe.ZRem(ctx, familyCooldownIndexKey(accountID), family)
	_, _ = pipe.Exec(ctx)
}

// ClearAccount 清除账号下所有家族冷却。管理员刷新额度或手动解除限流标记时使用。
func (fc *FamilyCooldown) ClearAccount(ctx context.Context, accountID int) int {
	if fc == nil || fc.rdb == nil {
		return 0
	}
	indexKey := familyCooldownIndexKey(accountID)
	families, err := fc.rdb.ZRange(ctx, indexKey, 0, -1).Result()
	if err != nil {
		return 0
	}
	pipe := fc.rdb.Pipeline()
	if len(families) > 0 {
		keys := make([]string, 0, len(families))
		for _, family := range families {
			keys = append(keys, familyCooldownReasonKey(accountID, family))
		}
		pipe.Del(ctx, keys...)
	}
	pipe.Del(ctx, indexKey)
	_, _ = pipe.Exec(ctx)
	return len(families)
}

// FamilyCooldownEntry 描述一条仍在生效的家族冷却。给后台展示用。
type FamilyCooldownEntry struct {
	Family string
	Until  time.Time
	Reason string
}

// List 列出指定账号当前所有家族冷却。供后台账号管理页展示用。
func (fc *FamilyCooldown) List(ctx context.Context, accountID int) []FamilyCooldownEntry {
	batch := fc.ListBatch(ctx, []int{accountID})
	return batch[accountID]
}

// ListBatch 批量列出多个账号当前所有家族冷却。
//
// 冷却展示走账号索引 ZSET，不扫描 Redis keyspace。账号列表自动刷新时，当前页 100 个账号
// 会变成 100 个 ZRANGEBYSCORE 管道命令，而不是 100 次 SCAN 全库游标遍历。
func (fc *FamilyCooldown) ListBatch(ctx context.Context, accountIDs []int) map[int][]FamilyCooldownEntry {
	if fc == nil || fc.rdb == nil {
		return nil
	}
	ids := uniqueAccountIDs(accountIDs)
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	minScore := fmt.Sprintf("(%d", now.UnixMilli())

	readPipe := fc.rdb.Pipeline()
	rangeCmds := make(map[int]*redis.ZSliceCmd, len(ids))
	for _, accountID := range ids {
		rangeCmds[accountID] = readPipe.ZRangeByScoreWithScores(ctx, familyCooldownIndexKey(accountID), &redis.ZRangeBy{
			Min: minScore,
			Max: "+inf",
		})
	}
	if _, err := readPipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil
	}

	type pendingReason struct {
		accountID int
		family    string
		until     time.Time
		cmd       *redis.StringCmd
	}
	result := make(map[int][]FamilyCooldownEntry)
	pending := make([]pendingReason, 0)
	reasonPipe := fc.rdb.Pipeline()
	for accountID, cmd := range rangeCmds {
		items, err := cmd.Result()
		if err != nil {
			continue
		}
		for _, item := range items {
			family, ok := item.Member.(string)
			if !ok || family == "" {
				continue
			}
			until := time.UnixMilli(int64(item.Score))
			if !until.After(now) {
				continue
			}
			pending = append(pending, pendingReason{
				accountID: accountID,
				family:    family,
				until:     until,
				cmd:       reasonPipe.Get(ctx, familyCooldownReasonKey(accountID, family)),
			})
		}
	}
	if len(pending) == 0 {
		return result
	}
	_, _ = reasonPipe.Exec(ctx)

	var stale []pendingReason
	for _, item := range pending {
		reason, err := item.cmd.Result()
		if err == redis.Nil {
			stale = append(stale, item)
			continue
		}
		if err != nil {
			continue
		}
		result[item.accountID] = append(result[item.accountID], FamilyCooldownEntry{
			Family: item.family,
			Until:  item.until,
			Reason: reason,
		})
	}
	if len(stale) > 0 {
		cleanup := fc.rdb.Pipeline()
		for _, item := range stale {
			cleanup.ZRem(ctx, familyCooldownIndexKey(item.accountID), item.family)
		}
		_, _ = cleanup.Exec(ctx)
	}
	return result
}

func uniqueAccountIDs(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
