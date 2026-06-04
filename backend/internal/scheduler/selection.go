package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
)

// SelectAccount 选一个可用账户。流程：
//
//	模型路由 → 状态过滤 → 软约束过滤（RPM / window / session）→
//	硬续链亲和 → 同优先级软粘连 → 负载均衡。
//
// excludeIDs 为 failover 时已尝试过的账户。
func (s *Scheduler) SelectAccount(ctx context.Context, platform, model string, userID, groupID int, sessionID string, excludeIDs ...int) (*ent.Account, error) {
	return s.SelectAccountWithOptions(ctx, platform, model, userID, groupID, sessionID, AccountSelectionOptions{}, excludeIDs...)
}

type AccountSelectionOptions struct {
	PreviousResponseID          string
	RequireContinuationAffinity bool
}

// SelectAccountWithOptions 在常规调度前优先按 previous_response_id 命中原账号。
// RequireContinuationAffinity=true 时，请求不是自包含重放，previous_response_id 或 session sticky
// 都是硬亲和，不能被 priority 覆盖。普通 session sticky 只在当前可用最高优先级层内生效。
func (s *Scheduler) SelectAccountWithOptions(ctx context.Context, platform, model string, userID, groupID int, sessionID string, opts AccountSelectionOptions, excludeIDs ...int) (*ent.Account, error) {
	candidates, err := s.routeAccounts(ctx, platform, model, groupID)
	if err != nil {
		return nil, err
	}
	if candidates = excludeAccounts(candidates, excludeIDs); len(candidates) == 0 {
		return nil, ErrNoAvailableAccount
	}
	if fn, ok := s.accountFilters[platform]; ok {
		if candidates = fn(candidates, model); len(candidates) == 0 {
			return nil, ErrNoAvailableAccount
		}
	}

	now := time.Now()
	snapshot := s.newSelectionSnapshot(ctx, candidates, model, now)
	snapshot.loadSchedulability(ctx, s, s.deferredConstraintCandidates(ctx, candidates, model, now, snapshot))
	normalCandidates := make([]*ent.Account, 0, len(candidates))
	stickyCandidates := make([]*ent.Account, 0, len(candidates))
	var hardAffinityCandidates []*ent.Account
	if opts.RequireContinuationAffinity {
		hardAffinityCandidates = make([]*ent.Account, 0, len(candidates))
	}
	for _, acc := range candidates {
		result := s.checkSchedulabilityResult(ctx, acc, model, now, opts.RequireContinuationAffinity, snapshot)
		switch result.normal {
		case Normal:
			normalCandidates = append(normalCandidates, acc)
			stickyCandidates = append(stickyCandidates, acc)
		case StickyOnly:
			stickyCandidates = append(stickyCandidates, acc)
		}
		if opts.RequireContinuationAffinity && result.hardAffinity != NotSchedulable {
			hardAffinityCandidates = append(hardAffinityCandidates, acc)
		}
	}

	if previousResponseID := strings.TrimSpace(opts.PreviousResponseID); previousResponseID != "" && s.responseAffinity != nil {
		if accountID, found := s.responseAffinity.Get(ctx, groupID, platform, previousResponseID); found {
			if opts.RequireContinuationAffinity {
				if acc := findAccountByID(hardAffinityCandidates, accountID); acc != nil {
					s.responseAffinity.Refresh(ctx, groupID, platform, previousResponseID, accountID)
					if sessionID != "" {
						s.sticky.Set(ctx, userID, platform, sessionID, accountID)
					}
					return acc, nil
				}
				return nil, continuationBlockedError(candidates, accountID)
			}
			if acc := selectSoftStickyAccount(softStickyCandidates(normalCandidates, stickyCandidates), accountID); acc != nil {
				s.responseAffinity.Refresh(ctx, groupID, platform, previousResponseID, accountID)
				if sessionID != "" {
					s.sticky.Set(ctx, userID, platform, sessionID, accountID)
				}
				return acc, nil
			}
			return nil, ErrPreviousResponseAffinitySkip
		}
	}

	// 续链请求的 session sticky 是硬亲和；普通 session sticky 只是软粘连，
	// 低优先级旧账号不能抢过当前可用最高优先级账号。
	if sessionID != "" {
		if accountID, found := s.sticky.Get(ctx, userID, platform, sessionID); found {
			if opts.RequireContinuationAffinity {
				if acc := findAccountByID(hardAffinityCandidates, accountID); acc != nil {
					s.sticky.Set(ctx, userID, platform, sessionID, accountID)
					return acc, nil
				}
				return nil, continuationBlockedError(candidates, accountID)
			} else if acc := selectSoftStickyAccount(softStickyCandidates(normalCandidates, stickyCandidates), accountID); acc != nil {
				s.sticky.Set(ctx, userID, platform, sessionID, accountID)
				return acc, nil
			}
		}
	}
	if opts.RequireContinuationAffinity {
		return nil, ErrContinuationAffinityMissing
	}

	normalSelectionCandidates, stickySelectionCandidates := prioritySelectionCandidates(normalCandidates, stickyCandidates)
	if len(normalSelectionCandidates) == 0 {
		// 没有 Normal 但可能有 StickyOnly 兜底（如 degraded 账号）
		if len(stickySelectionCandidates) == 0 {
			return nil, ErrNoAvailableAccount
		}
		selected := s.selectByLoadBalance(ctx, stickySelectionCandidates, now, snapshot)
		if selected == nil {
			return nil, ErrNoAvailableAccount
		}
		slog.Warn("scheduler_fallback_degraded_account",
			sdk.LogFieldAccountID, selected.ID,
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldModel, model,
		)
		return s.maybeRegisterSession(ctx, selected, userID, platform, sessionID, stickySelectionCandidates, now, snapshot)
	}

	selected := s.selectByLoadBalance(ctx, normalSelectionCandidates, now, snapshot)
	if selected == nil {
		return nil, ErrNoAvailableAccount
	}
	return s.maybeRegisterSession(ctx, selected, userID, platform, sessionID, normalSelectionCandidates, now, snapshot)
}

func continuationBlockedError(candidates []*ent.Account, accountID int) error {
	if findAccountByID(candidates, accountID) != nil {
		return ErrContinuationCapacityExceeded
	}
	return ErrContinuationAffinityMissing
}

func findAccountByID(candidates []*ent.Account, accountID int) *ent.Account {
	if accountID <= 0 {
		return nil
	}
	for _, acc := range candidates {
		if acc != nil && acc.ID == accountID {
			return acc
		}
	}
	return nil
}

func softStickyCandidates(normalCandidates, stickyCandidates []*ent.Account) []*ent.Account {
	normalPool, stickyPool := prioritySelectionCandidates(normalCandidates, stickyCandidates)
	if len(normalPool) > 0 {
		return normalPool
	}
	return stickyPool
}

func prioritySelectionCandidates(normalCandidates, stickyCandidates []*ent.Account) ([]*ent.Account, []*ent.Account) {
	normalNonNegative := filterPriorityCandidates(normalCandidates, false)
	stickyNonNegative := filterPriorityCandidates(stickyCandidates, false)
	if len(normalNonNegative) > 0 || len(stickyNonNegative) > 0 {
		return normalNonNegative, stickyNonNegative
	}
	return filterPriorityCandidates(normalCandidates, true), filterPriorityCandidates(stickyCandidates, true)
}

func filterPriorityCandidates(candidates []*ent.Account, negative bool) []*ent.Account {
	filtered := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if acc == nil {
			continue
		}
		if (acc.Priority < 0) == negative {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

func selectSoftStickyAccount(candidates []*ent.Account, accountID int) *ent.Account {
	acc := findAccountByID(candidates, accountID)
	if acc == nil {
		return nil
	}
	maxPriority := acc.Priority
	for _, candidate := range candidates {
		if candidate != nil && candidate.Priority > maxPriority {
			maxPriority = candidate.Priority
		}
	}
	if acc.Priority != maxPriority {
		return nil
	}
	return acc
}

// excludeAccounts 过滤掉 excludeIDs 中的账号（failover 已尝试过的）。
func excludeAccounts(candidates []*ent.Account, excludeIDs []int) []*ent.Account {
	if len(excludeIDs) == 0 {
		return candidates
	}
	excludeSet := make(map[int]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		excludeSet[id] = struct{}{}
	}
	filtered := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if _, excluded := excludeSet[acc.ID]; !excluded {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

// maybeRegisterSession 有 sessionID 时登记会话；session 数超限换一个候选重试。
func (s *Scheduler) maybeRegisterSession(ctx context.Context, selected *ent.Account, userID int, platform, sessionID string, pool []*ent.Account, now time.Time, snapshot *selectionSnapshot) (*ent.Account, error) {
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
	selected = s.selectByLoadBalance(ctx, retry, now, snapshot)
	if selected == nil || !s.RegisterSession(ctx, selected.ID, sessionID, selected.Extra) {
		return nil, ErrNoAvailableAccount
	}
	s.sticky.Set(ctx, userID, platform, sessionID, selected.ID)
	return selected, nil
}

// routeAccounts 取分组下匹配模型路由的账号；状态过滤延到 checkSchedulability。
//
// 不按 state 过滤的原因：新账号刚解除 disabled 后可立即被调度，不用等缓存失效。
//
// 首层命中 routeCache（key = (groupID, platform)）；miss 才查 DB。Model routing
// 规则与账号列表一起缓存，按 model 过滤的动作每次都重新跑——避免"不同 model 复用同一条缓存"
// 带来的错配。
func (s *Scheduler) routeAccounts(ctx context.Context, platform, model string, groupID int) ([]*ent.Account, error) {
	if accounts, routing, ok := s.routeCache.Get(groupID, platform); ok {
		return applyModelRouting(accounts, routing, model), nil
	}

	grp, err := s.db.Group.Get(ctx, groupID)
	if err != nil {
		return nil, normalizeGroupLookupError(err)
	}

	accounts, err := grp.QueryAccounts().
		Where(account.PlatformEQ(platform)).
		WithProxy().
		All(ctx)
	if err != nil {
		return nil, normalizeGroupAccountsLookupError(err)
	}

	// 缓存全量 platform 账号（包含所有 state）+ group 的 ModelRouting
	s.routeCache.Set(groupID, platform, accounts, grp.ModelRouting)

	return applyModelRouting(accounts, grp.ModelRouting, model), nil
}

func normalizeGroupLookupError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if ent.IsNotFound(err) {
		return fmt.Errorf("%w: %v", ErrGroupNotFound, err)
	}
	return fmt.Errorf("查询分组失败: %w", err)
}

func normalizeGroupAccountsLookupError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("查询分组账户失败: %w", err)
}

// applyModelRouting 按 model 过滤候选账号。routing 为 nil/空时原样返回；有规则但未命中时无候选。
func applyModelRouting(accounts []*ent.Account, routing map[string][]int64, model string) []*ent.Account {
	if len(routing) == 0 {
		return accounts
	}
	allowedIDs := matchModelRouting(routing, model)
	if allowedIDs == nil {
		return nil
	}
	if len(allowedIDs) == 0 {
		return nil
	}
	idSet := make(map[int64]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		idSet[id] = true
	}
	// 不能原地复用 accounts slice：那是缓存共享的底层数组，别处还在读
	filtered := make([]*ent.Account, 0, len(accounts))
	for _, acc := range accounts {
		if idSet[int64(acc.ID)] {
			filtered = append(filtered, acc)
		}
	}
	return filtered
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

// checkSchedulability 先看状态（state + state_until），再叠加软约束（并发 / windowCost / RPM / session），取最严格者。
// model 用于推导请求所属的家族（gpt-image / chat 各算一个池），仅当该家族正在
// 冷却时才把账号当作 NotSchedulable —— 别的家族不受影响。
func (s *Scheduler) checkSchedulability(ctx context.Context, acc *ent.Account, model string, now time.Time) Schedulability {
	return s.checkSchedulabilityResult(ctx, acc, model, now, false, nil).normal
}

type schedulabilityResult struct {
	normal       Schedulability
	hardAffinity Schedulability
}

// checkHardAffinitySchedulability 用于 previous_response_id / continuation session 这类硬亲和。
// 它只放宽滑动窗口费用的硬上限：该容量会随时间窗口滚动自动降回阈值内。
// 不放宽 disabled / rate_limited / family cooldown / RPM / 并发 / session 等保护。
func (s *Scheduler) checkHardAffinitySchedulability(ctx context.Context, acc *ent.Account, model string, now time.Time) Schedulability {
	return s.checkSchedulabilityResult(ctx, acc, model, now, true, nil).hardAffinity
}

func (s *Scheduler) checkSchedulabilityResult(ctx context.Context, acc *ent.Account, model string, now time.Time, needHardAffinity bool, snapshot *selectionSnapshot) schedulabilityResult {
	base := SchedulabilityOf(acc, now)
	if base == NotSchedulable {
		return schedulabilityResult{normal: NotSchedulable, hardAffinity: NotSchedulable}
	}
	result := schedulabilityResult{normal: base, hardAffinity: base}

	// 家族级冷却：撞过这个 family 的账号在冷却期内对该 family 不可调度，
	// 但对其它 family 仍可用。Redis 不可用时退化为不冷却，不阻断主链路。
	if family := ModelFamily(acc.Platform, model); family != "" && s.familyCooldown != nil {
		inCooldown, fromSnapshot := snapshot.inFamilyCooldown(acc.ID)
		if !fromSnapshot {
			_, inCooldown = s.familyCooldown.Until(ctx, acc.ID, family)
		}
		if inCooldown {
			return schedulabilityResult{normal: NotSchedulable, hardAffinity: NotSchedulable}
		}
	}

	if sched := s.concurrencySchedulability(ctx, acc, snapshot); sched > result.normal {
		result.normal = sched
		result.hardAffinity = sched
	}
	if result.normal == NotSchedulable {
		return result
	}

	windowSched, fromSnapshot := snapshot.windowCostSchedulability(acc.ID)
	if !fromSnapshot {
		windowSched = s.windowCost.GetSchedulability(ctx, acc.ID, acc.Extra)
	}
	if windowSched > result.normal {
		result.normal = windowSched
		hardAffinitySched := windowSched
		if needHardAffinity && hardAffinitySched == NotSchedulable {
			hardAffinitySched = StickyOnly
		}
		if hardAffinitySched > result.hardAffinity {
			result.hardAffinity = hardAffinitySched
		}
	}
	if result.normal == NotSchedulable && (!needHardAffinity || result.hardAffinity == NotSchedulable) {
		return result
	}

	rpmSched, fromSnapshot := snapshot.rpmSchedulability(acc.ID)
	if !fromSnapshot {
		rpmSched = s.rpm.GetSchedulability(ctx, acc.ID, ExtraInt(acc.Extra, "max_rpm"))
	}
	if result.normal != NotSchedulable && rpmSched > result.normal {
		result.normal = rpmSched
	}
	if rpmSched > result.hardAffinity {
		result.hardAffinity = rpmSched
	}
	if result.normal == NotSchedulable && (!needHardAffinity || result.hardAffinity == NotSchedulable) {
		return result
	}

	sessionSched, fromSnapshot := snapshot.sessionSchedulability(acc.ID)
	if !fromSnapshot {
		sessionSched = s.session.GetSchedulability(ctx, acc.ID, acc.Extra)
	}
	if result.normal != NotSchedulable && sessionSched > result.normal {
		result.normal = sessionSched
	}
	if sessionSched > result.hardAffinity {
		result.hardAffinity = sessionSched
	}
	return result
}

// concurrencySchedulability 根据当前并发用量返回调度约束：
//
//	load >= 100% → NotSchedulable（调度器直接跳过，避免下游 acquireSlot 失败浪费 failover）
//	load >=  80% → StickyOnly（软降级：只有粘性会话能选中，新请求优先换账号）
//	否则         → Normal
//
// 存在 TOCTOU（这里看没满、下一瞬 acquireSlot 却满）：forwarder 会 failover 到下一个账号兜底。
func (s *Scheduler) concurrencySchedulability(ctx context.Context, acc *ent.Account, snapshot *selectionSnapshot) Schedulability {
	maxConc := acc.MaxConcurrency
	if maxConc <= 0 {
		maxConc = DefaultAccountMaxConcurrency
	}
	load := snapshot.currentLoad(s, ctx, acc.ID)
	if load >= maxConc {
		return NotSchedulable
	}
	if float64(load)/float64(maxConc) >= 0.8 {
		return StickyOnly
	}
	return Normal
}

// selectByLoadBalance 严格按优先级分层：只从最高优先级层选账号，
// 同层内按 (1-load)*100 + lru_score 打分做加权随机。
//
// 低优先级账号只有在高优先级全部被 checkSchedulability 过滤掉后才能被选中。
// 负优先级沿用同一规则：只要有 >=0 的可调度账号，就不会进入负优先级兜底层。
// 同层内从 top-N 随机选一个，避免高并发下全部命中同一账号。
func (s *Scheduler) selectByLoadBalance(ctx context.Context, candidates []*ent.Account, now time.Time, snapshot *selectionSnapshot) *ent.Account {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	// 找到最高优先级，只保留该层候选
	maxPriority := candidates[0].Priority
	for _, acc := range candidates[1:] {
		if acc.Priority > maxPriority {
			maxPriority = acc.Priority
		}
	}
	tier := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if acc.Priority == maxPriority {
			tier = append(tier, acc)
		}
	}
	if len(tier) == 1 {
		return tier[0]
	}

	// 同优先级内按负载 + LRU 打分
	type scored struct {
		acc   *ent.Account
		score float64
	}
	items := make([]scored, 0, len(tier))

	for _, acc := range tier {
		maxConc := acc.MaxConcurrency
		if maxConc <= 0 {
			maxConc = DefaultAccountMaxConcurrency
		}
		loadRate := float64(snapshot.currentLoad(s, ctx, acc.ID)) / float64(maxConc)
		if loadRate > 1 {
			loadRate = 1
		}

		lruScore := 100.0
		if acc.LastUsedAt != nil {
			if elapsed := now.Sub(*acc.LastUsedAt).Minutes(); elapsed < 100 {
				lruScore = elapsed
			}
		}
		items = append(items, scored{
			acc:   acc,
			score: (1-loadRate)*100 + lruScore,
		})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	const maxTopN = 32
	topN := len(items)
	if topN > maxTopN {
		topN = maxTopN
	}
	return items[rand.Intn(topN)].acc
}

// getCurrentLoad 读取 acquire/release 维护的账号并发 count key。
//
// count key 与 slot key 使用相同短 TTL；请求异常未 release 时，count 最晚随 TTL 过期。
func (s *Scheduler) getCurrentLoad(ctx context.Context, accountID int) int {
	if s.currentLoad != nil {
		return s.currentLoad(ctx, accountID)
	}
	if s.rdb == nil {
		return 0
	}
	counts := loadConcurrencyCounts(ctx, s.rdb, []int{accountID})
	return counts[accountID]
}
