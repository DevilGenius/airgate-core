package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
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
	GroupNameSnapshot           string
	// PreferDifferentAccountType 是 failover 的尽力偏好：存在其它正常可用类型时，
	// 只在其它类型中选号；只有当前类型可用时自动回退，不会制造无账号错误。
	PreferDifferentAccountType string
}

// SelectAccountWithOptions 在常规调度前优先按 previous_response_id 命中原账号。
// RequireContinuationAffinity=true 时，请求不是自包含重放，previous_response_id 或 session sticky
// 都是硬亲和，不能被 priority 覆盖。普通 session sticky 只在当前可用最高优先级层内生效。
func (s *Scheduler) SelectAccountWithOptions(ctx context.Context, platform, model string, userID, groupID int, sessionID string, opts AccountSelectionOptions, excludeIDs ...int) (selected *ent.Account, err error) {
	defer func() {
		if errors.Is(err, ErrNoAvailableAccount) {
			s.recordNoAvailableAccount(ctx, platform, model, userID, groupID, sessionID, opts, excludeIDs)
		}
	}()

	candidates, err := s.routeAccounts(platform, model, groupID)
	if err != nil {
		return nil, err
	}
	if candidates = excludeAccounts(candidates, excludeIDs); len(candidates) == 0 {
		return nil, ErrNoAvailableAccount
	}
	now := time.Now()
	if previousResponseID := strings.TrimSpace(opts.PreviousResponseID); previousResponseID != "" && s.responseAffinity != nil {
		if selected, handled, err := s.selectPreviousResponseAffinity(ctx, platform, model, userID, groupID, sessionID, candidates, opts, previousResponseID, now); handled {
			return selected, err
		}
	}

	snapshot := s.loadedSelectionSnapshot(ctx, candidates, model, now)
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
	if !opts.RequireContinuationAffinity {
		normalCandidates, stickyCandidates = preferDifferentAccountTypeCandidates(
			normalCandidates,
			stickyCandidates,
			opts.PreferDifferentAccountType,
		)
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
		selected = s.selectByLoadBalance(ctx, stickySelectionCandidates, now, snapshot)
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

	selected = s.selectByLoadBalance(ctx, normalSelectionCandidates, now, snapshot)
	if selected == nil {
		return nil, ErrNoAvailableAccount
	}
	return s.maybeRegisterSession(ctx, selected, userID, platform, sessionID, normalSelectionCandidates, now, snapshot)
}

func (s *Scheduler) recordNoAvailableAccount(ctx context.Context, platform, model string, userID, groupID int, sessionID string, opts AccountSelectionOptions, excludeIDs []int) {
	if s == nil || s.state == nil || s.state.monitor == nil {
		return
	}
	if len(excludeIDs) > 0 {
		return
	}
	subjectID := platform
	if groupID > 0 {
		subjectID = strconv.Itoa(groupID)
	}
	detail := map[string]interface{}{
		"exclude_count": len(excludeIDs),
		"group_id":      groupID,
		"model":         model,
		"platform":      platform,
	}
	if opts.GroupNameSnapshot != "" {
		detail["group_name"] = opts.GroupNameSnapshot
	}
	if userID > 0 {
		detail["user_id"] = userID
	}
	if sessionID != "" {
		detail["has_session"] = true
	}
	if opts.PreviousResponseID != "" {
		detail["has_previous_response_id"] = true
	}
	if opts.RequireContinuationAffinity {
		detail["require_continuation_affinity"] = true
	}
	s.state.monitor.Record(ctx, monitoring.EventInput{
		Type:        monitoring.TypeSchedulerError,
		Severity:    monitoring.SeverityError,
		Source:      monitoring.SourceScheduler,
		SubjectType: monitoring.SubjectScheduler,
		SubjectID:   subjectID,
		Platform:    platform,
		ErrorCode:   "no_available_account",
		Title:       "No available account",
		Message:     "Scheduler could not find an available upstream account",
		Detail:      detail,
	})
}

func (s *Scheduler) selectPreviousResponseAffinity(
	ctx context.Context,
	platform string,
	model string,
	userID int,
	groupID int,
	sessionID string,
	candidates []*ent.Account,
	opts AccountSelectionOptions,
	previousResponseID string,
	now time.Time,
) (*ent.Account, bool, error) {
	accountID, found := s.responseAffinity.Get(ctx, groupID, platform, previousResponseID)
	if !found {
		return nil, false, nil
	}
	acc := findAccountByID(candidates, accountID)
	if acc == nil {
		if opts.RequireContinuationAffinity {
			return nil, true, ErrContinuationAffinityMissing
		}
		return nil, true, ErrPreviousResponseAffinitySkip
	}

	result := s.checkSchedulabilityForAccount(ctx, acc, model, now, opts.RequireContinuationAffinity)
	if opts.RequireContinuationAffinity {
		if result.hardAffinity != NotSchedulable {
			s.refreshPreviousResponseAffinity(ctx, groupID, platform, previousResponseID, accountID, userID, sessionID)
			return acc, true, nil
		}
		return nil, true, continuationBlockedError(candidates, accountID)
	}
	if result.normal == NotSchedulable {
		return nil, true, ErrPreviousResponseAffinitySkip
	}
	if s.softPreviousResponseAffinityAllowed(ctx, candidates, acc, result.normal, model, now) {
		s.refreshPreviousResponseAffinity(ctx, groupID, platform, previousResponseID, accountID, userID, sessionID)
		return acc, true, nil
	}
	// 被当前最高优先级可用层阻挡时直接交给 forwarder 恢复：删除 previous_response_id
	// 后重新进入正常调度。这里不复用快路径的局部 snapshot，避免跨恢复路径传递易过期状态。
	return nil, true, ErrPreviousResponseAffinitySkip
}

func (s *Scheduler) refreshPreviousResponseAffinity(ctx context.Context, groupID int, platform, previousResponseID string, accountID int, userID int, sessionID string) {
	s.responseAffinity.Refresh(ctx, groupID, platform, previousResponseID, accountID)
	if sessionID != "" {
		s.sticky.Set(ctx, userID, platform, sessionID, accountID)
	}
}

func (s *Scheduler) checkSchedulabilityForAccount(ctx context.Context, acc *ent.Account, model string, now time.Time, needHardAffinity bool) schedulabilityResult {
	snapshot := s.loadedSelectionSnapshot(ctx, []*ent.Account{acc}, model, now)
	return s.checkSchedulabilityResult(ctx, acc, model, now, needHardAffinity, snapshot)
}

func (s *Scheduler) softPreviousResponseAffinityAllowed(ctx context.Context, candidates []*ent.Account, affinity *ent.Account, affinitySched Schedulability, model string, now time.Time) bool {
	competitors := softAffinityCompetitors(candidates, affinity, affinitySched)
	if len(competitors) == 0 {
		return true
	}
	snapshot := s.loadedSelectionSnapshot(ctx, competitors, model, now)
	for _, acc := range competitors {
		result := s.checkSchedulabilityResult(ctx, acc, model, now, false, snapshot)
		if softAffinityCompetitorBlocks(affinity, affinitySched, acc, result.normal) {
			return false
		}
	}
	return true
}

// softAffinityCompetitors 只保留可能阻挡 soft affinity 的账号：
//   - affinity >= 0 且 Normal：非负、更高优先级账号
//   - affinity >= 0 且 StickyOnly：非负账号；同级或低级 StickyOnly 后续不会阻挡
//   - affinity < 0 且 Normal：所有非负账号，以及更高优先级的负数账号
//   - affinity < 0 且 StickyOnly：所有非负和负数账号；负数 Normal 即使优先级更低也会阻挡
//
// 最终是否阻挡由 softAffinityCompetitorBlocks 按实际 schedulability 判断。
func softAffinityCompetitors(candidates []*ent.Account, affinity *ent.Account, affinitySched Schedulability) []*ent.Account {
	if affinity == nil {
		return nil
	}
	out := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		if acc == nil || acc.ID == affinity.ID {
			continue
		}
		if affinity.Priority >= 0 {
			if acc.Priority < 0 {
				continue
			}
			if affinitySched == Normal && acc.Priority <= affinity.Priority {
				continue
			}
			out = append(out, acc)
			continue
		}
		if affinitySched == Normal && acc.Priority < 0 && acc.Priority <= affinity.Priority {
			continue
		}
		out = append(out, acc)
	}
	return out
}

func softAffinityCompetitorBlocks(affinity *ent.Account, affinitySched Schedulability, competitor *ent.Account, competitorSched Schedulability) bool {
	if affinity == nil || competitor == nil || competitorSched == NotSchedulable {
		return false
	}
	if affinity.Priority >= 0 {
		if competitor.Priority < 0 {
			return false
		}
		if affinitySched == Normal {
			return competitorSched == Normal && competitor.Priority > affinity.Priority
		}
		return competitorSched == Normal || (competitorSched == StickyOnly && competitor.Priority > affinity.Priority)
	}
	if competitor.Priority >= 0 {
		return competitorSched == Normal || competitorSched == StickyOnly
	}
	if affinitySched == Normal {
		return competitorSched == Normal && competitor.Priority > affinity.Priority
	}
	return competitorSched == Normal || (competitorSched == StickyOnly && competitor.Priority > affinity.Priority)
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

// AccountFailoverType 返回账号轮换使用的精确类型标识。
// OAuth 账号优先使用套餐字段，并刻意不应用 routegraph 的类别别名，确保 k12 与 team
// 在第三次 failover 时被视为不同类型；其它账号至少按实体 Type 区分。
func AccountFailoverType(acc *ent.Account) string {
	if acc == nil {
		return ""
	}
	baseType := normalizeFailoverAccountType(acc.Type)
	for _, key := range []string{"plan_type", "plan", "account_type", "account_category", "subscription_type"} {
		value := strings.TrimSpace(acc.Credentials[key])
		if value == "" {
			value = strings.TrimSpace(ExtraString(acc.Extra, key))
		}
		subtype := normalizeFailoverAccountType(value)
		if subtype == "" || subtype == baseType {
			continue
		}
		if baseType == "" {
			return subtype
		}
		return baseType + ":" + subtype
	}
	return baseType
}

func normalizeFailoverAccountType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func preferDifferentAccountTypeCandidates(normalCandidates, stickyCandidates []*ent.Account, previousType string) ([]*ent.Account, []*ent.Account) {
	previousType = strings.TrimSpace(previousType)
	if previousType == "" {
		return normalCandidates, stickyCandidates
	}

	preferredNormal := filterDifferentAccountType(normalCandidates, previousType)
	if len(preferredNormal) > 0 {
		return preferredNormal, filterDifferentAccountType(stickyCandidates, previousType)
	}
	// 不用其它类型的 StickyOnly 账号抢占当前类型的 Normal 账号；只有本来就没有
	// Normal 候选时，才在 StickyOnly 兜底池中应用类型偏好。
	if len(normalCandidates) == 0 {
		if preferredSticky := filterDifferentAccountType(stickyCandidates, previousType); len(preferredSticky) > 0 {
			return normalCandidates, preferredSticky
		}
	}
	return normalCandidates, stickyCandidates
}

func filterDifferentAccountType(candidates []*ent.Account, previousType string) []*ent.Account {
	filtered := make([]*ent.Account, 0, len(candidates))
	for _, acc := range candidates {
		candidateType := AccountFailoverType(acc)
		if candidateType != "" && candidateType != previousType {
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

// routeAccounts 从 RouteGraph 取分组账号并应用静态模型策略。
// RouteGraph 是调度热路径的权威来源；miss 表示启动快照未就绪或分组不存在。
func (s *Scheduler) routeAccounts(platform, model string, groupID int) ([]*ent.Account, error) {
	groupNode := routegraph.Group(groupID)
	if groupNode == nil {
		return nil, ErrGroupNotFound
	}
	if groupNode.Platform != platform {
		return nil, ErrNoAvailableAccount
	}
	return groupNode.AccountsForModel(model), nil
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
// 它放宽滑动窗口费用和 degraded 兜底，让原续链账号有机会由上游确认。
// 已知本地冷却态不再探测原账号，交给上层恢复为换账号尝试。
// 不放宽 disabled / RPM / 并发 / session 等本地保护。
func (s *Scheduler) checkHardAffinitySchedulability(ctx context.Context, acc *ent.Account, model string, now time.Time) Schedulability {
	return s.checkSchedulabilityResult(ctx, acc, model, now, true, nil).hardAffinity
}

func (s *Scheduler) checkSchedulabilityResult(ctx context.Context, acc *ent.Account, model string, now time.Time, needHardAffinity bool, snapshot *selectionSnapshot) schedulabilityResult {
	acc = s.stateCache.Apply(acc)
	base := schedulabilityWithTransientAvoidance(acc, now)
	hardBase := base
	if needHardAffinity {
		hardBase = hardAffinitySchedulabilityWithTransientAvoidance(acc, now)
	}
	if base == NotSchedulable && (!needHardAffinity || hardBase == NotSchedulable) {
		return schedulabilityResult{normal: NotSchedulable, hardAffinity: NotSchedulable}
	}
	result := schedulabilityResult{normal: base, hardAffinity: hardBase}

	// 家族级冷却：撞过这个 family 的账号在冷却期内对该 family 不可调度，
	// 但对其它 family 仍可用。Redis 不可用时退化为不冷却，不阻断主链路。
	if family := ModelFamily(acc.Platform, model); family != "" && s.familyCooldown != nil {
		inCooldown, fromSnapshot := snapshot.inFamilyCooldown(acc.ID)
		if !fromSnapshot {
			_, inCooldown = s.familyCooldown.Until(ctx, acc.ID, family)
		}
		if inCooldown {
			result.normal = NotSchedulable
			result.hardAffinity = NotSchedulable
		}
		if result.normal == NotSchedulable && (!needHardAffinity || result.hardAffinity == NotSchedulable) {
			return result
		}
	}

	sched := s.concurrencySchedulability(ctx, acc, snapshot)
	if sched > result.normal {
		result.normal = sched
	}
	if sched > result.hardAffinity {
		result.hardAffinity = sched
	}
	if result.normal == NotSchedulable && (!needHardAffinity || result.hardAffinity == NotSchedulable) {
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

func hardAffinityBaseSchedulability(acc *ent.Account, now time.Time) Schedulability {
	if acc == nil {
		return NotSchedulable
	}
	switch acc.State {
	case account.StateActive:
		return Normal
	case account.StateDisabled:
		return NotSchedulable
	case account.StateRateLimited:
		if acc.StateUntil != nil && acc.StateUntil.After(now) {
			return NotSchedulable
		}
		return Normal
	case account.StateDegraded:
		if acc.StateUntil != nil && acc.StateUntil.After(now) {
			return StickyOnly
		}
		return Normal
	default:
		return NotSchedulable
	}
}

// concurrencySchedulability 根据当前并发用量返回调度约束：
//
//	load >= 100% → NotSchedulable（调度器直接跳过，避免下游 acquireSlot 失败浪费 failover）
//	load >=  80% → StickyOnly（只有粘性会话能选中，新请求优先换账号）
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
	counts := loadConcurrencyCounts(ctx, s.rdb, []int{accountID}, true)
	return counts[accountID]
}
