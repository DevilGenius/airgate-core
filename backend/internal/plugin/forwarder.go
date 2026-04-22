package plugin

import (
	"log/slog"
	"net/http"
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

// maxFailoverAttempts 最大 failover 次数（账号级失败后切换新账号上游调用的上限）。
const maxFailoverAttempts = 3

// queueWaitTimeout 所有账号 slot 都被占满时，请求最多排队等多久再放弃。
// 1 分钟对号池小 / 并发高的场景能把毛刺吸收掉；超过这个时长意味着号池真的不够用。
const queueWaitTimeout = 60 * time.Second

// queuePollInterval slot 未释放时的轮询间隔。
const queuePollInterval = 200 * time.Millisecond

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

	// 两类排除：
	//   hardExclude —— callPlugin 后判为账号级失败的账号；整条 forward 不再重选它
	//   softExclude —— acquireAccountSlot 因 slot race/RPM/并发 满失败；短暂跳过它，
	//                  等 slot 释放或到 queueDeadline 再重新考虑
	var hardExclude []int
	var softExclude []int
	var mwBag map[string]string
	beginCalled := false
	attempt := 0

	ctx := c.Request.Context()
	queueDeadline := time.Now().Add(queueWaitTimeout)

	for attempt < maxFailoverAttempts {
		exclude := make([]int, 0, len(hardExclude)+len(softExclude))
		exclude = append(exclude, hardExclude...)
		exclude = append(exclude, softExclude...)

		if err := f.pickAccount(c, state, exclude...); err != nil {
			// 所有账号都被排除了。如果其中部分只是被 slot race 软排除，给它们时间释放。
			if len(softExclude) > 0 && time.Now().Before(queueDeadline) {
				softExclude = nil // 清软排除，让下轮重新考虑
				select {
				case <-ctx.Done():
					return
				case <-time.After(queuePollInterval):
				}
				continue
			}
			slog.Warn("账户调度失败",
				"platform", state.plugin.Platform,
				"model", state.model,
				"hard_excluded", hardExclude,
				"soft_excluded", softExclude,
				"error", err)
			openAIError(c, http.StatusServiceUnavailable, "server_error", "no_available_account", "无可用账户")
			return
		}

		accountID := state.account.ID
		releaseAccountSlot, ok := f.acquireAccountSlot(c, state)
		if !ok {
			// slot race：软排除此账号，下一轮选别的；等所有账号都 soft 排再排队等待。
			softExclude = append(softExclude, accountID)
			continue
		}

		if !beginCalled {
			allowed, bag := f.runForwardBeginChain(c, state)
			beginCalled = true
			if !allowed {
				f.scheduler.DecrementRPM(ctx, accountID)
				releaseAccountSlot()
				return
			}
			mwBag = bag
		}

		execution := f.callPlugin(c, state)
		attempt++

		if f.canFailover(c, state, execution) {
			// 失败被 failover 吞掉时没人写日志——状态机对 UpstreamTransient 非池账号是 no-op，
			// writeResult 又不会走到这里。必须在这里打，否则排查 503 时根本看不到上游真正的错。
			slog.Warn("账号调用失败，尝试 failover",
				"plugin", state.plugin.Name,
				"account_id", accountID,
				"attempt", attempt,
				"kind", execution.outcome.Kind,
				"upstream_status", execution.outcome.Upstream.StatusCode,
				"duration_ms", execution.duration.Milliseconds(),
				"reason", judgmentReason(execution),
				"error", execution.err)
			releaseAccountSlot()
			f.applyOutcome(ctx, state, execution)

			// 账号级故障（Dead / RateLimited）→ hardExclude，永不回选
			// 上游瞬时故障（UpstreamTransient，含上游自己的 502）→ softExclude，下一轮可重选
			//   意味着单账号号池下也能对上游的 "please retry" 做真正的重试。
			if execution.outcome.Kind.IsAccountFault() {
				hardExclude = append(hardExclude, accountID)
			} else {
				softExclude = append(softExclude, accountID)
			}
			continue
		}

		f.runForwardEndChain(c, state, execution, mwBag)
		f.writeResult(c, state, execution)
		releaseAccountSlot()
		return
	}

	slog.Warn("所有 failover 尝试均失败",
		"plugin", state.plugin.Name,
		"model", state.model,
		"attempts", attempt,
		"tried_accounts", hardExclude)
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
