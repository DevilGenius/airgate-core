package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"time"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/account"
)

// SelectAccount 选一个可用账户。流程：
//
//	模型路由 → 状态过滤 → 软约束过滤（RPM / window / session）→
//	粘性会话 → 负载均衡。
//
// excludeIDs 为 failover 时已尝试过的账户。
func (s *Scheduler) SelectAccount(ctx context.Context, platform, model string, userID, groupID int, sessionID string, excludeIDs ...int) (*ent.Account, error) {
	candidates, err := s.routeAccounts(ctx, platform, model, groupID)
	if err != nil {
		return nil, err
	}
	if candidates = excludeAccounts(candidates, excludeIDs); len(candidates) == 0 {
		return nil, ErrNoAvailableAccount
	}

	now := time.Now()
	var normalCandidates, stickyCandidates []*ent.Account
	for _, acc := range candidates {
		switch s.checkSchedulability(ctx, acc, now) {
		case Normal:
			normalCandidates = append(normalCandidates, acc)
			stickyCandidates = append(stickyCandidates, acc)
		case StickyOnly:
			stickyCandidates = append(stickyCandidates, acc)
		case NotSchedulable:
			// 跳过
		}
	}

	// 粘性会话优先（可命中 StickyOnly + Normal）
	if sessionID != "" {
		if accountID, found := s.sticky.Get(ctx, userID, platform, sessionID); found {
			for _, acc := range stickyCandidates {
				if acc.ID == accountID {
					s.sticky.Set(ctx, userID, platform, sessionID, accountID)
					return acc, nil
				}
			}
		}
	}

	if len(normalCandidates) == 0 {
		// 没有 Normal 但可能有 StickyOnly 兜底（如 degraded 账号）
		if len(stickyCandidates) == 0 {
			return nil, ErrNoAvailableAccount
		}
		selected := s.selectByLoadBalance(ctx, stickyCandidates, now)
		if selected == nil {
			return nil, ErrNoAvailableAccount
		}
		slog.Warn("无正常账号，兜底使用降级账号", "account_id", selected.ID)
		return s.maybeRegisterSession(ctx, selected, userID, platform, sessionID, stickyCandidates, now)
	}

	selected := s.selectByLoadBalance(ctx, normalCandidates, now)
	if selected == nil {
		return nil, ErrNoAvailableAccount
	}
	return s.maybeRegisterSession(ctx, selected, userID, platform, sessionID, normalCandidates, now)
}

// excludeAccounts 过滤掉 excludeIDs 中的账号（failover 已尝试过的）。
func excludeAccounts(candidates []*ent.Account, excludeIDs []int) []*ent.Account {
	if len(excludeIDs) == 0 {
		return candidates
	}
	excludeSet := make(map[int]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		excludeSet[id] = true
	}
	filtered := candidates[:0]
	for _, acc := range candidates {
		if !excludeSet[acc.ID] {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

// maybeRegisterSession 有 sessionID 时登记会话；session 数超限换一个候选重试。
func (s *Scheduler) maybeRegisterSession(ctx context.Context, selected *ent.Account, userID int, platform, sessionID string, pool []*ent.Account, now time.Time) (*ent.Account, error) {
	if sessionID == "" {
		return selected, nil
	}
	if s.RegisterSession(ctx, selected.ID, sessionID, selected.Extra) {
		s.sticky.Set(ctx, userID, platform, sessionID, selected.ID)
		return selected, nil
	}
	retry := pool[:0]
	for _, acc := range pool {
		if acc.ID != selected.ID {
			retry = append(retry, acc)
		}
	}
	if len(retry) == 0 {
		return nil, ErrNoAvailableAccount
	}
	selected = s.selectByLoadBalance(ctx, retry, now)
	if selected == nil || !s.RegisterSession(ctx, selected.ID, sessionID, selected.Extra) {
		return nil, ErrNoAvailableAccount
	}
	s.sticky.Set(ctx, userID, platform, sessionID, selected.ID)
	return selected, nil
}

// routeAccounts 取分组下匹配模型路由的账号；状态过滤延到 checkSchedulability。
//
// 不按 state 过滤的原因：新账号刚解除 disabled 后可立即被调度，不用等缓存失效。
func (s *Scheduler) routeAccounts(ctx context.Context, platform, model string, groupID int) ([]*ent.Account, error) {
	grp, err := s.db.Group.Get(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGroupNotFound, err)
	}

	accounts, err := grp.QueryAccounts().
		Where(account.PlatformEQ(platform)).
		WithProxy().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询分组账户失败: %w", err)
	}

	if len(grp.ModelRouting) == 0 {
		return accounts, nil
	}
	allowedIDs := matchModelRouting(grp.ModelRouting, model)
	if len(allowedIDs) == 0 {
		return accounts, nil
	}

	idSet := make(map[int64]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		idSet[id] = true
	}
	filtered := accounts[:0]
	for _, acc := range accounts {
		if idSet[int64(acc.ID)] {
			filtered = append(filtered, acc)
		}
	}
	return filtered, nil
}

// matchModelRouting 匹配模型路由规则，返回允许的账号 ID 列表。nil 或空表示不限制。
func matchModelRouting(routing map[string][]int64, model string) []int64 {
	if ids, ok := routing[model]; ok {
		return ids
	}
	for pattern, ids := range routing {
		if matched, _ := filepath.Match(pattern, model); matched {
			return ids
		}
	}
	return nil
}

// checkSchedulability 先看状态（state + state_until），再叠加软约束（windowCost / RPM / session），取最严格者。
func (s *Scheduler) checkSchedulability(ctx context.Context, acc *ent.Account, now time.Time) Schedulability {
	base := SchedulabilityOf(acc, now)
	if base == NotSchedulable {
		return NotSchedulable
	}
	worst := base

	if sched := s.windowCost.GetSchedulability(ctx, acc.ID, acc.Extra); sched > worst {
		worst = sched
	}
	if worst == NotSchedulable {
		return worst
	}
	if sched := s.rpm.GetSchedulability(ctx, acc.ID, ExtraInt(acc.Extra, "max_rpm")); sched > worst {
		worst = sched
	}
	if worst == NotSchedulable {
		return worst
	}
	if sched := s.session.GetSchedulability(ctx, acc.ID, acc.Extra); sched > worst {
		worst = sched
	}
	return worst
}

// selectByLoadBalance 按 priority × 1000 + (1-load)×100 + lru_score 打分挑最高。
func (s *Scheduler) selectByLoadBalance(ctx context.Context, candidates []*ent.Account, now time.Time) *ent.Account {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	type scored struct {
		acc   *ent.Account
		score float64
	}
	items := make([]scored, 0, len(candidates))

	for _, acc := range candidates {
		maxConc := acc.MaxConcurrency
		if maxConc <= 0 {
			maxConc = 5
		}
		loadRate := float64(s.getCurrentLoad(ctx, acc.ID)) / float64(maxConc)
		if loadRate > 1 {
			loadRate = 1
		}

		lruScore := 100.0 // 从未使用过 → 最高
		if acc.LastUsedAt != nil {
			if elapsed := now.Sub(*acc.LastUsedAt).Minutes(); elapsed < 100 {
				lruScore = elapsed
			}
		}
		items = append(items, scored{
			acc:   acc,
			score: float64(acc.Priority)*1000 + (1-loadRate)*100 + lruScore,
		})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })
	return items[0].acc
}

// getCurrentLoad 从 Redis SET 读账号当前并发数。
func (s *Scheduler) getCurrentLoad(ctx context.Context, accountID int) int {
	if s.rdb == nil {
		return 0
	}
	n, err := s.rdb.SCard(ctx, fmt.Sprintf("concurrency:%d", accountID)).Result()
	if err != nil {
		return 0
	}
	return int(n)
}
