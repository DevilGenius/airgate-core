package plugin

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
)

// Forwarder 请求转发器：认证 → 余额预检 → 调度 → 并发闸门 → 转发 → 判决 → 计费 → 记录。
type Forwarder struct {
	db          *ent.Client
	manager     *Manager
	scheduler   *scheduler.Scheduler
	concurrency *scheduler.ConcurrencyManager
	calculator  *billing.Calculator
	recorder    *billing.Recorder
}

// NewForwarder 创建转发器。
func NewForwarder(
	db *ent.Client,
	manager *Manager,
	sched *scheduler.Scheduler,
	concurrency *scheduler.ConcurrencyManager,
	calculator *billing.Calculator,
	recorder *billing.Recorder,
) *Forwarder {
	return &Forwarder{
		db:          db,
		manager:     manager,
		scheduler:   sched,
		concurrency: concurrency,
		calculator:  calculator,
		recorder:    recorder,
	}
}

// maxFailoverAttempts 最大 failover 次数（含首次）。
const maxFailoverAttempts = 3

// Forward 入口。失败时自动 failover 到其它账号，最多 maxFailoverAttempts 次。
//
// Middleware：OnForwardBegin 只在首次 attempt 调用（避免 failover 污染审计计数），
// OnForwardEnd 在最终一次 attempt（成功或放弃）触发，LIFO 降序。Begin DENY 会拒绝请求。
func (f *Forwarder) Forward(c *gin.Context) {
	state, ok := f.parseRequest(c)
	if !ok {
		return
	}
	if !f.checkBalance(c, state) {
		return
	}

	// 只读元信息快车道：插件本地合成响应，跳过整条账号 / 闸门 / failover 链路。
	if isMetadataOnlyPath(state.requestPath) {
		f.forwardMetadataOnly(c, state)
		return
	}

	releaseClientQuota := f.acquireClientQuota(c, state)
	if releaseClientQuota == nil {
		return // 429 已写
	}
	defer releaseClientQuota()

	var excludeIDs []int
	var mwBag map[string]string
	beginCalled := false

	for attempt := 0; attempt < maxFailoverAttempts; attempt++ {
		if !f.pickAccount(c, state, excludeIDs...) {
			return
		}
		excludeIDs = append(excludeIDs, state.account.ID)

		releaseAccountSlot, ok := f.acquireAccountSlot(c, state)
		if !ok {
			return // 429 已写
		}

		if !beginCalled {
			allowed, bag := f.runForwardBeginChain(c, state)
			beginCalled = true
			if !allowed {
				// DENY 未调用上游，必须回退本次 attempt 占用的 RPM 配额。
				f.scheduler.DecrementRPM(c.Request.Context(), state.account.ID)
				releaseAccountSlot()
				return
			}
			mwBag = bag
		}

		execution := f.callPlugin(c, state)

		if attempt < maxFailoverAttempts-1 && f.canFailover(c, state, execution) {
			releaseAccountSlot()
			f.applyOutcome(c.Request.Context(), state, execution)
			continue
		}

		// 最终结果：OnForwardEnd 先于 writeResult，保证 middleware 看到的 metadata
		// 与 usage_log 写入是同一份事实。
		f.runForwardEndChain(c, state, execution, mwBag)
		f.writeResult(c, state, execution)
		releaseAccountSlot()
		return
	}

	// failover 用尽。OnForwardEnd 已在最后一次 attempt 触发过，这里只做兜底响应。
	openAIError(c, 503, "server_error", "all_accounts_failed", "所有可用账户均失败，请稍后重试")
}

// canFailover 是否允许换账号重试。
// 流式已写入 → 不可；err 非 nil（插件自身崩）→ 可；其余由 Kind.ShouldFailover() 决定。
func (f *Forwarder) canFailover(c *gin.Context, state *forwardState, execution forwardExecution) bool {
	if state.stream && c.Writer.Written() {
		return false
	}
	if execution.err != nil {
		return true
	}
	return execution.outcome.Kind.ShouldFailover()
}

// callPlugin 把请求发给插件。
func (f *Forwarder) callPlugin(c *gin.Context, state *forwardState) forwardExecution {
	outcome, err := state.plugin.Gateway.Forward(c.Request.Context(), buildPluginRequest(c, state))
	return forwardExecution{
		outcome:  outcome,
		err:      err,
		duration: time.Since(state.startedAt),
	}
}
