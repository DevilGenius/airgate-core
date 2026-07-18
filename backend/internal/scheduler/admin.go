package scheduler

import (
	"context"
	"time"

	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/accountscope"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// 管理员 / 配额巡检的状态写入口。这些调用不经过 Apply —— 它们是"外部已知事实"
// 的直接落库，不需要 RPM 回退、失败计数等逻辑。

// ApplyAccountTestOutcome 使用与正常调度请求相同的账号状态机判定处理管理员账号测试结果。
// 账号测试没有先递增调度 RPM，因此直接进入 StateMachine，不能调用 Scheduler.Apply 的 RPM 回退。
func (s *Scheduler) ApplyAccountTestOutcome(ctx context.Context, accountID int, platform, model string, outcome sdk.ForwardOutcome, isPool bool) {
	if s == nil || s.state == nil || accountID <= 0 {
		return
	}
	s.state.Apply(ctx, accountID, Judgment{
		Kind:           outcome.Kind,
		RetryAfter:     outcome.RetryAfter,
		Reason:         outcome.Reason,
		Duration:       outcome.Duration,
		IsPool:         isPool,
		UpstreamStatus: outcome.Upstream.StatusCode,
		Family:         ModelFamily(platform, model),
	})
}

// ManualRecover 运维手动把账号恢复到 active：清状态、清到期、清原因，并立即刷新 RouteGraph。
func (s *Scheduler) ManualRecover(ctx context.Context, accountID int) error {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	upd := accountscope.UpdateOneID(s.db, accountID).
		SetState(account.StateActive).
		ClearStateUntil().
		SetErrorMsg("")
	if existing, getErr := accountscope.QueryByID(s.db, accountID).Only(dbCtx); getErr == nil {
		if hasTransientAvoidanceExtra(existing.Extra) {
			extra := cloneExtra(existing.Extra)
			clearTransientAvoidanceExtra(extra)
			upd = upd.SetExtra(extra)
		}
	}

	err := upd.Exec(dbCtx)
	if err == nil {
		s.state.resolveAccountEvents(ctx, accountID)
		s.stateCache.Store(accountID, account.StateActive, nil, nil)
		s.RefreshRouteGraphAccount(ctx, accountID)
	}
	return err
}

// ManualDisable 运维手动禁用账号（语义等同自动 disabled，需要再次 ManualRecover 才能恢复）。
func (s *Scheduler) ManualDisable(ctx context.Context, accountID int, reason string) error {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	err := accountscope.UpdateOneID(s.db, accountID).
		SetState(account.StateDisabled).
		ClearStateUntil().
		SetErrorMsg(truncateReason(reason)).
		Exec(dbCtx)
	if err == nil {
		s.stateCache.Store(accountID, account.StateDisabled, nil, nil)
		s.RefreshRouteGraphAccount(ctx, accountID)
	}
	return err
}

// MarkRateLimited 配额巡检发现额度窗口已满时打入 rate_limited 直到 until。
func (s *Scheduler) MarkRateLimited(ctx context.Context, accountID int, until time.Time, reason string) {
	s.state.transition(ctx, accountID, account.StateRateLimited, &until, reason)
}

// ClearRateLimited 配额巡检发现已恢复时清限流态回到 active。
func (s *Scheduler) ClearRateLimited(ctx context.Context, accountID int) {
	s.state.transitionActive(ctx, accountID, true)
}

// ClearRateLimitMarkers 清除账号上的临时限流标记，不会恢复手动禁用的账号。
func (s *Scheduler) ClearRateLimitMarkers(ctx context.Context, accountID int) int {
	cleared := s.ClearFamilyCooldowns(ctx, accountID)
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	item, err := accountscope.QueryByID(s.db, accountID).Only(dbCtx)
	if err != nil {
		return cleared
	}
	if item.State == account.StateRateLimited || item.State == account.StateDegraded {
		s.state.transitionActive(ctx, accountID, true)
		cleared++
	}
	return cleared
}

// MarkDisabled 把账号标记为 disabled（凭证失效等确定性错误）。
func (s *Scheduler) MarkDisabled(ctx context.Context, accountID int, reason string) {
	s.state.transition(ctx, accountID, account.StateDisabled, nil, reason)
}

// MarkDegraded 把账号立即临时降级，不永久禁用；显式管理信号不走首次免退避。
func (s *Scheduler) MarkDegraded(ctx context.Context, accountID int, reason string) {
	s.state.applyTransientAvoidanceWithMinimumStep(ctx, accountID, Judgment{
		Kind:   sdk.OutcomeAccountUnavailable,
		Reason: reason,
	}, transientKindUnavailable, 1)
}
