package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// 状态机使用的默认窗口。
const (
	// rateLimitedDefault 上游没给 RetryAfter 时的兜底冷却。
	rateLimitedDefault = 60 * time.Second
	// rateLimitedMin 最小冷却下限。OpenAI 共享 org 限流时给的 RetryAfter 经常是 15ms~50ms，
	// 跟着这种瞬时值会让账号刚解锁就被同请求或并发请求立刻再撞墙。设一个下限让实际放出
	// 的请求拉开间隔，配合上游窗口老化才有效果。
	rateLimitedMin = 200 * time.Millisecond
	// rateLimitedMax OAuth 某些限流可能长达数天，设上限防止异常值。
	rateLimitedMax = 7 * 24 * time.Hour
	// transientAvoidance* 统一处理 403 临时不可用和 5xx/网络抖动：
	// 前三次写 degraded 短窗口并完全避让，第四次起写 60s degraded，调度变为 StickyOnly。
	transientAvoidanceFirst  = 7500 * time.Millisecond
	transientAvoidanceSecond = 15 * time.Second
	transientAvoidanceThird  = 30 * time.Second
	transientDegradedWindow  = 60 * time.Second

	// transient403DegradedThreshold 非池账号连续 403 进入 degraded 达到该次数后禁用。
	transient403DegradedThreshold = 3

	transientAvoidStepExtraKey   = "_airgate_transient_avoid_step"
	transient403DegradedCountKey = "_airgate_transient_403_degraded_count"
	transientKindUnavailable     = "account_unavailable"
	transientKindUpstream        = "upstream_transient"
)

// Judgment forwarder 对一次调用的判决，交给状态机做状态转移。
type Judgment struct {
	Kind           sdk.OutcomeKind
	RetryAfter     time.Duration
	Reason         string
	Duration       time.Duration // 仅用于日志 / 指标
	IsPool         bool          // 池账号（upstream_is_pool）仅在确定性账号死亡时保留部分豁免。
	Family         string        // 模型家族键（见 ModelFamily）。非空时 RateLimited 走 Redis 家族冷却而非账号级 DB state，避免 gpt-image 限流误伤 chat。
	UpstreamStatus int           // 上游 HTTP 状态码，用于池账号区分 401（自身凭证无效）和 403（透传上游错误）。
}

// StateMachine 账号状态机。所有状态转移必须通过 Apply 入口。
//
// 职责：
//   - 把 forwarder 的 Judgment 翻译成 DB 字段变更（state / state_until / error_msg / last_used_at）
//   - 关键转移（Active ↔ Disabled）通知上游清 route 缓存
//
// 确定性的账号级信号仍由 state 记录；临时 403 和 5xx 共享瞬时避让策略。
type StateMachine struct {
	db             *ent.Client
	familyCooldown *FamilyCooldown

	// onCriticalTransition Active ↔ Disabled 转移后的回调（由 Scheduler 注入）。
	// 用来清 route 缓存，让下次 SelectAccount 立刻看到新状态；
	// RateLimited / Degraded 这种"带 state_until 的临时状态"不走这里，由 TTL 兜底。
	onCriticalTransition func()
}

// NewStateMachine 构造状态机。fc 提供 (account, family) 维度的限流冷却，
// nil 时退化为旧行为：所有 RateLimited 都写账号级 DB state。
func NewStateMachine(db *ent.Client, fc *FamilyCooldown) *StateMachine {
	return &StateMachine{
		db:             db,
		familyCooldown: fc,
	}
}

// notifyCritical 发出关键状态变更事件。nil 回调时安静跳过。
func (sm *StateMachine) notifyCritical() {
	if sm.onCriticalTransition != nil {
		sm.onCriticalTransition()
	}
}

// Apply 把一次判决施加到账号状态机。只产生副作用，不返回要写给客户端的内容。
//
// 语义：
//
//	Success             → state=active，清 state_until，last_used_at=now
//	AccountRateLimited  → state=rate_limited，state_until=now+RetryAfter
//	AccountDead         → state=disabled（凭证失效，需人工介入）
//	AccountUnavailable  → 瞬时避让；非池账号连续 403 degraded 达阈值后 disabled
//	UpstreamTransient   → 瞬时避让；不会 disabled
//	ClientError / StreamAborted / Unknown → 不改状态（账号无辜）
func (sm *StateMachine) Apply(ctx context.Context, accountID int, j Judgment) {
	switch j.Kind {
	case sdk.OutcomeSuccess:
		sm.transitionActive(ctx, accountID, false)

	case sdk.OutcomeAccountRateLimited:
		dur := j.RetryAfter
		if dur <= 0 {
			dur = rateLimitedDefault
		}
		if dur < rateLimitedMin {
			dur = rateLimitedMin
		}
		if dur > rateLimitedMax {
			dur = rateLimitedMax
		}
		until := time.Now().Add(dur)
		// 有 Family 信息时走家族级冷却：撞 gpt-image 4000/min 不会让同账号 chat 被跳过。
		// 无 Family（admin 巡检 / 老插件）保留账号级 rate_limited 兜底，行为与改造前一致。
		if j.Family != "" && sm.familyCooldown != nil {
			sm.familyCooldown.Mark(ctx, accountID, j.Family, until, j.Reason)
			slog.Info("scheduler_family_cooldown",
				sdk.LogFieldAccountID, accountID,
				"family", j.Family,
				"until", until,
				sdk.LogFieldReason, j.Reason,
			)
			return
		}
		sm.transition(ctx, accountID, account.StateRateLimited, &until, j.Reason)

	case sdk.OutcomeAccountDead:
		if j.IsPool && j.UpstreamStatus != 401 {
			// 池账号的 403 等是上游透传的错误，池子本身没问题，
			// 不动状态，靠 failover 重试消化。
			// 401 表示池子自身的凭证无效，仍需禁用并说明原因。
			return
		}
		sm.transition(ctx, accountID, account.StateDisabled, nil, j.Reason)

	case sdk.OutcomeAccountUnavailable:
		sm.applyTransientAvoidance(ctx, accountID, j, transientKindUnavailable)

	case sdk.OutcomeUpstreamTransient:
		sm.applyTransientAvoidance(ctx, accountID, j, transientKindUpstream)

	case sdk.OutcomeClientError, sdk.OutcomeStreamAborted, sdk.OutcomeUnknown:
		// 账号无辜，不改状态。
	}
}

func (sm *StateMachine) applyTransientAvoidance(ctx context.Context, accountID int, j Judgment, transientKind string) {
	dbCtx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	existing, err := sm.db.Account.Get(dbCtx, accountID)
	if err != nil {
		slog.Warn("scheduler_transient_avoidance_load_failed",
			sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		return
	}

	now := time.Now()
	if existing.State == account.StateDisabled {
		slog.Warn("scheduler_transient_avoidance_ignored_disabled",
			sdk.LogFieldAccountID, accountID,
			"transient_kind", transientKind,
			sdk.LogFieldReason, j.Reason,
		)
		return
	}
	if existing.State == account.StateRateLimited && existing.StateUntil != nil && existing.StateUntil.After(now) {
		slog.Warn("scheduler_transient_avoidance_ignored_rate_limited",
			sdk.LogFieldAccountID, accountID,
			"transient_kind", transientKind,
			"until", existing.StateUntil,
			sdk.LogFieldReason, j.Reason,
		)
		return
	}
	if existing.State == account.StateRateLimited && isExpiredTemporaryState(existing, now) {
		slog.Debug("scheduler_transient_avoidance_expired_rate_limit_as_active",
			sdk.LogFieldAccountID, accountID,
			"until", existing.StateUntil,
			"transient_kind", transientKind,
			sdk.LogFieldReason, j.Reason,
		)
	}

	extra := cloneExtra(existing.Extra)
	step := extraInt(extra, transientAvoidStepExtraKey)
	delay, degraded := transientAvoidanceDelay(step)
	nextStep := nextTransientAvoidanceStep(step)
	until := now.Add(delay)
	extra[transientAvoidStepExtraKey] = nextStep

	if degraded && transientKind == transientKindUnavailable && !existing.UpstreamIsPool {
		count := extraInt(extra, transient403DegradedCountKey) + 1
		extra[transient403DegradedCountKey] = count
		if count >= transient403DegradedThreshold {
			clearTransientAvoidanceExtra(extra)
			err = sm.db.Account.UpdateOneID(accountID).
				SetState(account.StateDisabled).
				ClearStateUntil().
				SetErrorMsg(truncateReason(j.Reason)).
				SetExtra(extra).
				Exec(dbCtx)
			if err != nil {
				slog.Error("scheduler_transient_403_disable_failed",
					sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
				return
			}
			slog.Warn("scheduler_transient_403_disabled",
				sdk.LogFieldAccountID, accountID,
				"degraded_count", count,
				sdk.LogFieldReason, j.Reason,
			)
			sm.notifyCritical()
			return
		}
	} else {
		delete(extra, transient403DegradedCountKey)
	}

	err = sm.db.Account.UpdateOneID(accountID).
		SetState(account.StateDegraded).
		SetStateUntil(until).
		SetErrorMsg(truncateReason(j.Reason)).
		SetExtra(extra).
		Exec(dbCtx)
	if err != nil {
		slog.Error("scheduler_transient_degrade_failed",
			sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		return
	}
	slog.Warn("scheduler_transient_degraded",
		sdk.LogFieldAccountID, accountID,
		"transient_kind", transientKind,
		"step", nextStep,
		"until", until,
		"short_avoidance", !degraded,
		"degraded_403_count", extraInt(extra, transient403DegradedCountKey),
		sdk.LogFieldReason, j.Reason,
	)
}

func transientAvoidanceDelay(step int) (time.Duration, bool) {
	switch {
	case step <= 0:
		return transientAvoidanceFirst, false
	case step == 1:
		return transientAvoidanceSecond, false
	case step == 2:
		return transientAvoidanceThird, false
	default:
		return transientDegradedWindow, true
	}
}

func nextTransientAvoidanceStep(step int) int {
	if step < 0 {
		return 1
	}
	if step >= 3 {
		return 4
	}
	return step + 1
}

func clearTransientAvoidanceExtra(extra map[string]interface{}) {
	delete(extra, transientAvoidStepExtraKey)
	delete(extra, transient403DegradedCountKey)
}

func hasTransientAvoidanceExtra(extra map[string]interface{}) bool {
	if extra == nil {
		return false
	}
	if _, ok := extra[transientAvoidStepExtraKey]; ok {
		return true
	}
	return hasTransient403DegradedCount(extra)
}

func hasTransient403DegradedCount(extra map[string]interface{}) bool {
	if extra == nil {
		return false
	}
	_, ok := extra[transient403DegradedCountKey]
	return ok
}

// transitionActive 成功时回到 active：清 state_until、清 reason、清失败计数、更新 last_used_at。
//
// disabled 状态受保护：只有管理员操作（ManualRecover / ToggleScheduling）才能解除，
// forwarder 的 Success 判决不会覆盖它——防止在飞请求的成功回调把手动禁用的账号重新激活。
//
// force=false 时，未到期的 rate_limited / degraded 也受保护：成功判决只更新 last_used_at，
// 不提前结束完整冷却窗口。force=true 仅给管理员/配额巡检的显式清除入口使用。
func (sm *StateMachine) transitionActive(ctx context.Context, accountID int, force bool) {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	prevState := account.StateActive
	existing, getErr := sm.db.Account.Get(dbCtx, accountID)
	if getErr == nil {
		prevState = existing.State
	}

	if prevState == account.StateDisabled {
		err := sm.db.Account.UpdateOneID(accountID).
			SetLastUsedAt(now).
			Exec(dbCtx)
		if err != nil {
			slog.Debug("scheduler_state_success_ignored_disabled_touch_failed",
				sdk.LogFieldAccountID, accountID,
				sdk.LogFieldError, err,
			)
		} else {
			slog.Debug("scheduler_state_success_ignored_disabled",
				sdk.LogFieldAccountID, accountID,
			)
		}
		return
	}

	if !force && isUnexpiredTemporaryState(existing, now) {
		upd := sm.db.Account.UpdateOneID(accountID).
			SetLastUsedAt(now)
		if hasTransientAvoidanceExtra(existing.Extra) {
			extra := cloneExtra(existing.Extra)
			clearTransientAvoidanceExtra(extra)
			upd = upd.SetExtra(extra).
				SetErrorMsg("")
		}
		if err := upd.Exec(dbCtx); err != nil {
			slog.Warn("scheduler_state_active_touch_failed",
				sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		}
		return
	}

	upd := sm.db.Account.UpdateOneID(accountID).
		SetState(account.StateActive).
		ClearStateUntil().
		SetErrorMsg("").
		SetLastUsedAt(now)
	if getErr == nil {
		if hasTransientAvoidanceExtra(existing.Extra) {
			extra := cloneExtra(existing.Extra)
			clearTransientAvoidanceExtra(extra)
			upd = upd.SetExtra(extra)
		}
	}

	err := upd.Exec(dbCtx)
	if err != nil {
		slog.Warn("scheduler_state_active_failed",
			sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		return
	}
	if prevState != account.StateActive {
		sm.notifyCritical()
	}
}

// transition 把账号转到指定状态。stateUntil=nil 表示无到期（disabled）或清空。
func (sm *StateMachine) transition(ctx context.Context, accountID int, newState account.State, stateUntil *time.Time, reason string) {
	dbCtx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var existing *ent.Account
	if newState != account.StateDisabled {
		var err error
		existing, err = sm.db.Account.Get(dbCtx, accountID)
		if err == nil {
			if existing.State == account.StateDisabled {
				slog.Info("scheduler_state_transition_ignored_disabled",
					sdk.LogFieldAccountID, accountID,
					"target_state", newState,
					sdk.LogFieldReason, reason,
				)
				return
			}
			if newState == account.StateDegraded && existing.State == account.StateRateLimited &&
				existing.StateUntil != nil && existing.StateUntil.After(time.Now()) {
				slog.Info("scheduler_state_transition_ignored_rate_limited",
					sdk.LogFieldAccountID, accountID,
					"target_state", newState,
					"until", existing.StateUntil,
					sdk.LogFieldReason, reason,
				)
				return
			}
		}
	}

	upd := sm.db.Account.UpdateOneID(accountID).
		SetState(newState).
		SetErrorMsg(truncateReason(reason))
	if stateUntil == nil {
		upd = upd.ClearStateUntil()
	} else {
		upd = upd.SetStateUntil(*stateUntil)
	}
	if existing != nil && newState != account.StateDegraded && hasTransientAvoidanceExtra(existing.Extra) {
		extra := cloneExtra(existing.Extra)
		clearTransientAvoidanceExtra(extra)
		upd = upd.SetExtra(extra)
	}

	if err := upd.Exec(dbCtx); err != nil {
		slog.Error("scheduler_state_transition_failed",
			sdk.LogFieldAccountID, accountID,
			"target_state", newState,
			sdk.LogFieldError, err,
		)
		return
	}
	slog.Info("scheduler_state_transition",
		sdk.LogFieldAccountID, accountID,
		"state", newState,
		"until", stateUntil,
		sdk.LogFieldReason, reason,
	)

	// Disabled 是关键转移：缓存里还挂着 active 的快照会让调度器反复选它、白白浪费 failover。
	// RateLimited / Degraded 有 state_until，缓存 3s 陈旧期可接受。
	if newState == account.StateDisabled {
		sm.notifyCritical()
	}
}

func isUnexpiredTemporaryState(acc *ent.Account, now time.Time) bool {
	if acc == nil || acc.StateUntil == nil || !acc.StateUntil.After(now) {
		return false
	}
	return acc.State == account.StateRateLimited || acc.State == account.StateDegraded
}

func isExpiredTemporaryState(acc *ent.Account, now time.Time) bool {
	if acc == nil || acc.StateUntil == nil || acc.StateUntil.After(now) {
		return false
	}
	return acc.State == account.StateRateLimited || acc.State == account.StateDegraded
}

func isShortDegradedWindow(acc *ent.Account, now time.Time) bool {
	if acc == nil || acc.State != account.StateDegraded || acc.StateUntil == nil || !acc.StateUntil.After(now) {
		return false
	}
	step := extraInt(acc.Extra, transientAvoidStepExtraKey)
	return step > 0 && step < 4
}

func schedulabilityWithTransientAvoidance(acc *ent.Account, now time.Time) Schedulability {
	sched := SchedulabilityOf(acc, now)
	if sched == StickyOnly && isShortDegradedWindow(acc, now) {
		return NotSchedulable
	}
	return sched
}

func hardAffinitySchedulabilityWithTransientAvoidance(acc *ent.Account, now time.Time) Schedulability {
	sched := hardAffinityBaseSchedulability(acc, now)
	if sched == StickyOnly && isShortDegradedWindow(acc, now) {
		return NotSchedulable
	}
	return sched
}

// SchedulabilityOf 根据当前状态 + 到期时间判断账号是否可调度。
//
// rate_limited / degraded 到期后**不会**自动写 DB（由下一次 Success 判决统一回收），
// 但调度器读到 state_until <= now 就会把它视为 active / StickyOnly，不再排除。
func SchedulabilityOf(acc *ent.Account, now time.Time) Schedulability {
	switch acc.State {
	case account.StateActive:
		return Normal
	case account.StateDisabled:
		return NotSchedulable
	case account.StateRateLimited:
		if acc.StateUntil != nil && acc.StateUntil.After(now) {
			return NotSchedulable
		}
		return Normal // 已到期，lazy 回收
	case account.StateDegraded:
		if acc.StateUntil != nil && acc.StateUntil.After(now) {
			return StickyOnly // 只在没有 Normal 账号时兜底
		}
		return Normal
	default:
		// 未知状态值：保守按不可用处理
		return NotSchedulable
	}
}

// truncateReason 限制 error_msg 长度，防止异常文本把列撑爆。
func truncateReason(s string) string {
	const maxLen = 500
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func cloneExtra(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func extraInt(extra map[string]interface{}, key string) int {
	switch v := extra[key].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}
