package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/ent/usagelog"
)

const (
	windowCostCacheTTL   = 30 * time.Second
	defaultWindowHours   = 5.0
	windowCostThreshold  = 0.8  // 80% 进入 StickyOnly
	defaultStickyReserve = 10.0 // 为粘性会话预留的额外额度
)

// WindowCostChecker 滑动窗口费用检查器
// 检查账户在最近 N 小时内的费用是否超过阈值
type WindowCostChecker struct {
	db  *ent.Client
	rdb *redis.Client
}

type windowCostLimit struct {
	MaxCost       float64
	WindowHours   float64
	StickyReserve float64
}

// NewWindowCostChecker 创建窗口费用检查器
func NewWindowCostChecker(db *ent.Client, rdb *redis.Client) *WindowCostChecker {
	return &WindowCostChecker{db: db, rdb: rdb}
}

// windowCostKey 生成 Redis 缓存键
func windowCostKey(accountID int) string {
	return fmt.Sprintf("ag:cost:window:%d", accountID)
}

// GetSchedulability 检查账户窗口费用调度状态
// max_window_cost <= 0 表示不限制
func (w *WindowCostChecker) GetSchedulability(ctx context.Context, accountID int, extra map[string]interface{}) Schedulability {
	limit, ok := windowCostLimitFromExtra(extra)
	if !ok {
		return Normal
	}
	cost, err := w.GetWindowCost(ctx, accountID, limit.WindowHours)
	if err != nil {
		slog.Debug("获取窗口费用失败，放行", "account_id", accountID, "error", err)
		return Normal // fail-open
	}
	return windowCostSchedulability(cost, limit)
}

// GetSchedulabilityBatch 批量检查窗口费用限制。Redis 命中时一次 MGET；miss 时按
// window_hours 分组后用 GROUP BY account_id 查询，避免每个候选账号单独聚合 DB。
func (w *WindowCostChecker) GetSchedulabilityBatch(ctx context.Context, accounts []*ent.Account) map[int]Schedulability {
	result := make(map[int]Schedulability, len(accounts))
	if w == nil || len(accounts) == 0 {
		return result
	}
	limits := make(map[int]windowCostLimit, len(accounts))
	ids := make([]int, 0, len(accounts))
	for _, acc := range accounts {
		if acc == nil {
			continue
		}
		limit, ok := windowCostLimitFromExtra(acc.Extra)
		if !ok {
			continue
		}
		limits[acc.ID] = limit
		ids = append(ids, acc.ID)
	}
	if len(ids) == 0 {
		return result
	}

	missing := ids
	if w.rdb != nil {
		keys := make([]string, 0, len(ids))
		for _, accountID := range ids {
			keys = append(keys, windowCostKey(accountID))
		}
		values, err := w.rdb.MGet(ctx, keys...).Result()
		if err == nil {
			missing = make([]int, 0)
			for index, value := range values {
				accountID := ids[index]
				cost, ok := redisFloatValue(value)
				if !ok {
					missing = append(missing, accountID)
					continue
				}
				result[accountID] = windowCostSchedulability(cost, limits[accountID])
			}
		}
	}

	if len(missing) == 0 || w.db == nil {
		return result
	}

	idsByWindow := make(map[float64][]int)
	for _, accountID := range missing {
		limit := limits[accountID]
		idsByWindow[limit.WindowHours] = append(idsByWindow[limit.WindowHours], accountID)
	}
	for windowHours, accountIDs := range idsByWindow {
		costs, err := w.loadWindowCosts(ctx, accountIDs, windowHours)
		if err != nil {
			slog.Debug("批量获取窗口费用失败，放行", "window_hours", windowHours, "error", err)
			continue
		}
		if w.rdb != nil {
			pipe := w.rdb.Pipeline()
			for _, accountID := range accountIDs {
				cost := costs[accountID]
				pipe.Set(ctx, windowCostKey(accountID), strconv.FormatFloat(cost, 'f', 8, 64), windowCostCacheTTL)
			}
			_, _ = pipe.Exec(ctx)
		}
		for _, accountID := range accountIDs {
			result[accountID] = windowCostSchedulability(costs[accountID], limits[accountID])
		}
	}
	return result
}

func windowCostLimitFromExtra(extra map[string]interface{}) (windowCostLimit, bool) {
	maxCost := ExtraFloat64(extra, "max_window_cost")
	if maxCost <= 0 {
		return windowCostLimit{}, false
	}
	windowHours := ExtraFloat64(extra, "window_hours")
	if windowHours <= 0 {
		windowHours = defaultWindowHours
	}
	stickyReserve := ExtraFloat64(extra, "sticky_reserve")
	if stickyReserve <= 0 {
		stickyReserve = defaultStickyReserve
	}
	return windowCostLimit{
		MaxCost:       maxCost,
		WindowHours:   windowHours,
		StickyReserve: stickyReserve,
	}, true
}

func windowCostSchedulability(cost float64, limit windowCostLimit) Schedulability {
	ratio := cost / limit.MaxCost
	if cost >= limit.MaxCost+limit.StickyReserve {
		return NotSchedulable // 超过预留额度，完全不可调度
	}
	if ratio >= windowCostThreshold {
		return StickyOnly // 接近上限，仅粘性会话
	}
	return Normal
}

// GetWindowCost 获取账户在指定窗口内的费用（带 Redis 缓存）
func (w *WindowCostChecker) GetWindowCost(ctx context.Context, accountID int, windowHours float64) (float64, error) {
	// 先查 Redis 缓存
	if w.rdb != nil {
		key := windowCostKey(accountID)
		val, err := w.rdb.Get(ctx, key).Result()
		if err == nil {
			if cost, parseErr := strconv.ParseFloat(val, 64); parseErr == nil {
				return cost, nil
			}
		}
	}

	// 缓存未命中，查数据库
	windowStart := time.Now().Add(-time.Duration(windowHours * float64(time.Hour)))

	var costs []struct {
		Sum float64 `json:"sum"`
	}

	err := w.db.UsageLog.Query().
		Where(
			usagelog.HasAccountWith(account.ID(accountID)),
			usagelog.CreatedAtGTE(windowStart),
		).
		Aggregate(ent.Sum(usagelog.FieldActualCost)).
		Scan(ctx, &costs)

	if err != nil {
		return 0, fmt.Errorf("查询窗口费用失败: %w", err)
	}

	cost := 0.0
	if len(costs) > 0 {
		cost = costs[0].Sum
	}

	// 写入 Redis 缓存
	if w.rdb != nil {
		key := windowCostKey(accountID)
		w.rdb.Set(ctx, key, strconv.FormatFloat(cost, 'f', 8, 64), windowCostCacheTTL)
	}

	return cost, nil
}

func (w *WindowCostChecker) loadWindowCosts(ctx context.Context, accountIDs []int, windowHours float64) (map[int]float64, error) {
	result := make(map[int]float64, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}
	windowStart := time.Now().Add(-time.Duration(windowHours * float64(time.Hour)))

	var rows []struct {
		AccountID int     `json:"account_usage_logs"`
		Sum       float64 `json:"sum"`
	}
	err := w.db.UsageLog.Query().
		Where(
			usagelog.HasAccountWith(account.IDIn(accountIDs...)),
			usagelog.CreatedAtGTE(windowStart),
		).
		GroupBy(usagelog.AccountColumn).
		Aggregate(ent.As(ent.Sum(usagelog.FieldActualCost), "sum")).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("查询窗口费用失败: %w", err)
	}
	for _, row := range rows {
		if row.AccountID > 0 {
			result[row.AccountID] = row.Sum
		}
	}
	return result, nil
}

func redisFloatValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case string:
		n, err := strconv.ParseFloat(v, 64)
		return n, err == nil
	case []byte:
		n, err := strconv.ParseFloat(string(v), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

// addCostScript 仅当 key 存在时增量更新，避免创建无 TTL 的 key
var addCostScript = redis.NewScript(`
	if redis.call('EXISTS', KEYS[1]) == 1 then
		return redis.call('INCRBYFLOAT', KEYS[1], ARGV[1])
	end
	return nil
`)

// AddCost 在请求计费后增量更新缓存的窗口费用
// 仅当缓存 key 存在时才增量更新，不存在则等下次 GetWindowCost 查询时从 DB 重建
func (w *WindowCostChecker) AddCost(ctx context.Context, accountID int, cost float64) {
	if w.rdb == nil || cost <= 0 {
		return
	}
	key := windowCostKey(accountID)
	addCostScript.Run(ctx, w.rdb, []string{key}, cost)
}
