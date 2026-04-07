package plugin

import (
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/ratelimit"
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
// 完整流程：认证 → 限流 → 余额预检 → 调度 → 并发控制 → 转发 → 计费 → 记录。
type Forwarder struct {
	db          *ent.Client
	manager     *Manager
	scheduler   *scheduler.Scheduler
	concurrency *scheduler.ConcurrencyManager
	limiter     *ratelimit.Limiter
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
		"websocket 连接失败: eof",
		"读取 websocket 消息失败: eof",
		"读取上游消息: eof",
		"读取客户端消息: eof",
		"websocket: close 1000",
		"websocket: close 1001",
	}
	for _, needle := range ignored {
		if strings.Contains(msg, needle) {
			return false
		}
	}
	return true
}

// canFailover 判断本次转发失败是否可以切换账户重试
// 条件：1) 响应未开始写入（流式场景）2) 错误类型可重试（429、连接错误、5xx）
func (f *Forwarder) canFailover(c *gin.Context, state *forwardState, execution forwardExecution) bool {
	// 流式模式下，如果已经开始向客户端写入数据，不能重试
	if state.stream && c.Writer.Written() {
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
	limiter *ratelimit.Limiter,
	calculator *billing.Calculator,
	recorder *billing.Recorder,
) *Forwarder {
	return &Forwarder{
		db:          db,
		manager:     manager,
		scheduler:   sched,
		concurrency: concurrency,
		limiter:     limiter,
		calculator:  calculator,
		recorder:    recorder,
	}
}

// maxFailoverAttempts 最大 failover 次数（首次 + 重试）
const maxFailoverAttempts = 3

// Forward 转发请求到对应插件。
// 支持账户级 failover：当遇到可重试错误（429、连接失败等）且响应未开始写入时，
// 自动切换到其他账户重试，最多尝试 maxFailoverAttempts 次。
func (f *Forwarder) Forward(c *gin.Context) {
	state, ok := f.buildForwardState(c)
	if !ok {
		return
	}

	if !f.ensureForwardAllowed(c, state) {
		return
	}

	// 记录已尝试过的账户，failover 时排除
	triedAccountIDs := make(map[int]bool)

	for attempt := 0; attempt < maxFailoverAttempts; attempt++ {
		if !f.selectForwardAccount(c, state) {
			return
		}

		// 如果此账户已尝试过，跳过（防止重复选中同一账户）
		if triedAccountIDs[state.account.ID] {
			break
		}
		triedAccountIDs[state.account.ID] = true

		cleanup, ok := f.prepareForwardExecution(c, state)
		if !ok {
			return
		}

		execution := f.executeForward(c, state)

		// 判断是否可以 failover
		if attempt < maxFailoverAttempts-1 && f.canFailover(c, state, execution) {
			// 释放当前账户资源，上报结果，然后重试
			cleanup()
			f.reportForwardExecution(c.Request.Context(), state, execution)
			continue
		}

		// 最终结果处理
		f.finishForward(c, state, execution)
		cleanup()
		return
	}

	// 所有 failover 都失败，返回最后一次错误
	openAIError(c, 503, "server_error", "all_accounts_failed", "所有可用账户均失败，请稍后重试")
}
