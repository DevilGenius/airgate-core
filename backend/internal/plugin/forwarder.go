package plugin

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk"
)

// openAIError 返回 OpenAI 兼容的错误格式，确保 Claude Code 等客户端能正确识别。
func openAIError(c *gin.Context, status int, errType, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	})
}

// Forwarder 请求转发器。
// 完整流程：认证 → 余额预检 → 调度 → API Key 并发 + 账号并发 → 转发 → 计费 → 记录。
type Forwarder struct {
	db          *ent.Client
	manager     *Manager
	scheduler   *scheduler.Scheduler
	concurrency *scheduler.ConcurrencyManager
	calculator  *billing.Calculator
	recorder    *billing.Recorder
}

// shouldPenalizeForwardError 判断转发失败是否应该计入账号失败次数。
// 像 WebSocket EOF / 正常关闭这类连接中断通常属于瞬时链路问题，不应导致账号被自动停用。
func shouldPenalizeForwardError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return false
	}

	msg := strings.ToLower(err.Error())
	ignored := []string{
		// WebSocket 正常断开
		"websocket 连接失败: eof",
		"读取 websocket 消息失败: eof",
		"读取上游消息: eof",
		"读取客户端消息: eof",
		"websocket: close 1000",
		"websocket: close 1001",
		// gRPC 基础设施错误（插件进程重启/crash），不应归咎账号
		"rpc error: code = unavailable",
		"error reading from server: eof",
		"transport is closing",
		"connection refused",
	}
	for _, needle := range ignored {
		if strings.Contains(msg, needle) {
			return false
		}
	}
	return true
}

// isClientSideForwardError 判断本次转发失败本质上是客户端请求自身的问题
// （如 model 不被支持、context 超长、参数无效等），与账号无关。
// 插件遇到这类错误时会回填 AccountStatus=OK + 4xx StatusCode 作为信号。
// 命中时既不应 failover 切账号，也不应计入账号失败次数。
func isClientSideForwardError(execution forwardExecution) bool {
	r := execution.result
	if r == nil {
		return false
	}
	return r.AccountStatus == sdk.AccountStatusOK &&
		r.StatusCode >= 400 && r.StatusCode < 500
}

// canFailover 判断本次转发失败是否可以切换账户重试
// 条件：1) 响应未开始写入（流式场景）2) 错误类型可重试（429、连接错误、5xx）
func (f *Forwarder) canFailover(c *gin.Context, state *forwardState, execution forwardExecution) bool {
	// 流式模式下，如果已经开始向客户端写入数据，不能重试
	if state.stream && c.Writer.Written() {
		return false
	}

	// 客户端请求自身的问题（不被支持的 model、超长 context 等），换账号也救不回来，
	// 而且不能让这类错误连续打多个账号、被误判为账号故障。
	if isClientSideForwardError(execution) {
		return false
	}

	// 连接级错误（超时、EOF 等），可以重试
	if execution.err != nil {
		return true
	}

	if execution.result == nil {
		return true
	}

	// 429 限流 — 可以换账户重试
	if execution.result.AccountStatus == sdk.AccountStatusRateLimited {
		return true
	}

	// 5xx 上游服务端错误 — 可以重试
	if execution.result.StatusCode >= 500 {
		return true
	}

	return false
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

// maxFailoverAttempts 最大 failover 次数（首次 + 重试）
const maxFailoverAttempts = 3

// Forward 转发请求到对应插件。
// 支持账户级 failover：当遇到可重试错误（429、连接失败等）且响应未开始写入时，
// 自动切换到其他账户重试，最多尝试 maxFailoverAttempts 次。
//
// Middleware 集成（详见 ADR-0001 Decision 2）：
//   - 选完账号、prepareForwardExecution 之前调 OnForwardBegin（升序）。
//     middleware 的 metadata bag 在整条链中累积传递。
//   - finishForward 之前调 OnForwardEnd（降序）。failover 重试不触发 End，
//     只有最终（成功 or 永久失败）的那一次才触发，避免对插件造成 N 倍噪声。
//   - middleware 失败永远不 block 主流程；唯一例外是 OnForwardBegin 返回 DENY。
func (f *Forwarder) Forward(c *gin.Context) {
	state, ok := f.buildForwardState(c)
	if !ok {
		return
	}

	if !f.ensureForwardAllowed(c, state) {
		return
	}

	// 只读元信息（/v1/models 等）快车道：
	// 这类请求由插件本地注册表合成响应，既不打上游也不产生计费。
	// 因此不需要账号调度、并发槽、RPM 限流、failover 重试等整条 forwarder 重链路——
	// 直接构造一个空账号的 ForwardRequest 调用插件即可。
	if isMetadataOnlyPath(state.requestPath) {
		f.forwardMetadataOnly(c, state)
		return
	}

	// 用户级 + API Key 级并发闸：两个闸门都可独立配置为 0 关闭。
	// 这里用独立于 state.requestID 的 slot ID，因为 state.requestID 在每次 failover
	// 的 prepareForwardExecution 里会被重新生成；而这两层槽位跨整个 Forward 请求稳定,
	// 需要一个独立 ID 保证 SADD/SREM 对得上。
	clientSlotRelease := f.acquireClientSlots(c, state)
	if clientSlotRelease == nil {
		// acquireClientSlots 已在失败路径写过 429 响应
		return
	}
	defer clientSlotRelease()

	// 记录已尝试过的账户 ID，传给调度器做排除
	var excludeIDs []int

	// middleware metadata bag：所有 middleware 在 begin/end 之间共享
	var mwBag map[string]string
	beginCalled := false

	for attempt := 0; attempt < maxFailoverAttempts; attempt++ {
		if !f.selectForwardAccount(c, state, excludeIDs...) {
			return
		}
		excludeIDs = append(excludeIDs, state.account.ID)

		cleanup, ok := f.prepareForwardExecution(c, state)
		if !ok {
			return
		}

		// middleware OnForwardBegin 链。只在第一次 attempt 调用——failover 切换账号
		// 不应该让 middleware 看到多次"开始"事件，那会污染审计日志的请求计数。
		if !beginCalled {
			allowed, bag := f.runForwardBeginChain(c, state)
			beginCalled = true
			if !allowed {
				cleanup()
				return
			}
			mwBag = bag
		}

		execution := f.executeForward(c, state)

		// 判断是否可以 failover
		if attempt < maxFailoverAttempts-1 && f.canFailover(c, state, execution) {
			// 释放当前账户资源，上报结果，然后重试
			cleanup()
			f.reportForwardExecution(c.Request.Context(), state, execution)
			continue
		}

		// 最终结果处理：先跑 middleware OnForwardEnd（用最终一次 execution 的数据），
		// 再走 finishForward 写 usage_log。这个顺序保证 middleware 看到的 metadata
		// 与 usage_log 计费写入是同一份事实，只是 middleware 观察更早一点点。
		f.runForwardEndChain(c, state, execution, mwBag)
		f.finishForward(c, state, execution)
		cleanup()
		return
	}

	// 所有 failover 都失败，返回最后一次错误。
	// 此时 OnForwardEnd 已经在最后一次 attempt 的失败分支被调过；这里的 503 是
	// 整个 forward 流程已耗尽重试次数后的兜底，不再额外触发 middleware。
	openAIError(c, 503, "server_error", "all_accounts_failed", "所有可用账户均失败，请稍后重试")
}

// forwardMetadataOnly 处理只读元信息请求（如 /v1/models）。
//
// 特点：
//   - 不需要账号：插件内部由本地注册表合成响应（openai 插件的 buildLocalModelsResponse）
//   - 不产生计费：不写 usage_log，也不走 middleware 的 begin/end 钩子
//   - 不占用并发槽和 RPM：这些闸门是用来保护上游账号的，本地合成的请求不涉及上游
//
// 失败场景：插件未运行、插件返回空响应 → 502。
func (f *Forwarder) forwardMetadataOnly(c *gin.Context, state *forwardState) {
	fwdReq := &sdk.ForwardRequest{
		// Account 留空：插件对 metadata 路径的判断在 account 访问之前发生
		Account: &sdk.Account{Platform: state.plugin.Platform},
		Body:    state.body,
		Headers: buildForwardHeaders(c.Request.Header, state.keyInfo),
		Model:   state.model,
		Stream:  false,
	}
	fwdReq.Headers.Set("X-Forwarded-Path", state.requestPath)
	fwdReq.Headers.Set("X-Forwarded-Method", c.Request.Method)

	result, err := state.plugin.Gateway.Forward(c.Request.Context(), fwdReq)
	if err != nil {
		slog.Error("metadata 请求插件失败",
			"plugin", state.plugin.Name,
			"path", state.requestPath,
			"error", err)
		openAIError(c, http.StatusBadGateway, "server_error", "upstream_error", "metadata 请求插件失败")
		return
	}
	if result == nil || len(result.Body) == 0 {
		openAIError(c, http.StatusBadGateway, "server_error", "upstream_error", "metadata 请求插件返回空响应")
		return
	}
	writeForwardResponse(c, result)
}
