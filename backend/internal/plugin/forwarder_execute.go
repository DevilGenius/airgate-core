package plugin

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	sdk "github.com/DouDOU-start/airgate-sdk"
)

// acquireAPIKeySlot 在 forward 路径最前面争抢 API Key 级并发槽。
//
//   - keyInfo.KeyMaxConcurrency <= 0 时直接返回 no-op release，表示该 key 不限制并发；
//   - 成功获取返回配对的 release 函数，Forward() 应 defer 调用；
//   - 争抢失败直接写 429 响应并返回 nil，调用方看到 nil 就应立即 return。
//
// 注意 slot ID 独立于 state.requestID，因为后者会在每次 failover attempt 里被重新生成；
// 而 API Key 槽位跨 attempt 稳定，必须有独立且稳定的成员 ID 保证 SREM 能匹配上。
func (f *Forwarder) acquireAPIKeySlot(c *gin.Context, state *forwardState) func() {
	maxConc := state.keyInfo.KeyMaxConcurrency
	if maxConc <= 0 {
		return func() {}
	}

	ctx := c.Request.Context()
	slotID := uuid.New().String()

	if err := f.concurrency.AcquireAPIKeySlot(ctx, state.keyInfo.KeyID, slotID, maxConc, 0); err != nil {
		openAIError(c, http.StatusTooManyRequests, "rate_limit_error", "apikey_concurrency_limit", "API Key 并发已达上限，请稍后重试")
		return nil
	}

	keyID := state.keyInfo.KeyID
	return func() {
		f.concurrency.ReleaseAPIKeySlot(ctx, keyID, slotID)
	}
}

func (f *Forwarder) ensureForwardAllowed(c *gin.Context, state *forwardState) bool {
	// 注：原本这里有一个硬编码 60 req/min 的用户级 RPM 限流，
	// 粒度太粗（同一个 user 的多把 key 共享一个配额）且无法配置，误伤严重，已移除。
	// 现在的限流层级：
	//   1. API Key 并发（可在管理面板按 key 单独设置，见 prepareForwardExecution）
	//   2. 账号级 max_rpm / 并发槽（在 prepareForwardExecution 里，保护上游）

	if state.keyInfo.UserBalance <= 0 {
		c.JSON(http.StatusPaymentRequired, gin.H{
			"error": gin.H{
				"message": "余额不足",
				"type":    "insufficient_quota",
				"code":    "insufficient_quota",
			},
		})
		return false
	}

	return true
}

func (f *Forwarder) selectForwardAccount(c *gin.Context, state *forwardState, excludeIDs ...int) bool {
	account, err := f.scheduler.SelectAccount(
		c.Request.Context(),
		state.plugin.Platform,
		state.model,
		state.keyInfo.UserID,
		state.keyInfo.GroupID,
		state.sessionID,
		excludeIDs...,
	)
	if err != nil {
		slog.Warn("账户调度失败", "platform", state.plugin.Platform, "model", state.model, "error", err)
		openAIError(c, http.StatusServiceUnavailable, "server_error", "no_available_account", "无可用账户")
		return false
	}

	state.account = account
	return true
}

func (f *Forwarder) prepareForwardExecution(c *gin.Context, state *forwardState) (func(), bool) {
	ctx := c.Request.Context()
	state.requestID = uuid.New().String()

	// 原子检查 RPM 限制并递增，防止并发请求超过 max_rpm
	maxRPM := scheduler.ExtraInt(state.account.Extra, "max_rpm")
	if !f.scheduler.TryIncrementRPM(ctx, state.account.ID, maxRPM) {
		openAIError(c, http.StatusTooManyRequests, "rate_limit_error", "rpm_limit", "账户 RPM 已达上限，请稍后重试")
		return nil, false
	}

	releaseMessageLock := func() {}
	if scheduler.IsRealUserMessage(state.body) {
		acquired, _ := f.scheduler.AcquireMessageLock(ctx, state.account.ID, state.requestID, state.account.Extra)
		if acquired {
			releaseMessageLock = func() {
				f.scheduler.ReleaseMessageLock(ctx, state.account.ID, state.requestID)
			}
			f.scheduler.EnforceMessageDelay(ctx, state.account.ID, state.account.Extra)
		}
	}

	maxConc := state.account.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 5
	}

	// 槽位 TTL 可通过 extra["slot_ttl_seconds"] 配置，默认 300 秒（5 分钟）
	slotTTL := time.Duration(scheduler.ExtraInt(state.account.Extra, "slot_ttl_seconds")) * time.Second

	if err := f.concurrency.AcquireSlot(ctx, state.account.ID, state.requestID, maxConc, slotTTL); err != nil {
		releaseMessageLock()
		f.scheduler.DecrementRPM(ctx, state.account.ID)
		openAIError(c, http.StatusTooManyRequests, "rate_limit_error", "concurrency_limit", "并发已满，请稍后重试")
		return nil, false
	}

	return func() {
		f.concurrency.ReleaseSlot(ctx, state.account.ID, state.requestID)
		releaseMessageLock()
	}, true
}

func (f *Forwarder) executeForward(c *gin.Context, state *forwardState) forwardExecution {
	result, err := state.plugin.Gateway.Forward(c.Request.Context(), buildForwardRequest(c, state))
	return forwardExecution{
		result:   result,
		err:      err,
		duration: time.Since(state.startedAt),
	}
}

func buildForwardRequest(c *gin.Context, state *forwardState) *sdk.ForwardRequest {
	headers := buildForwardHeaders(c.Request.Header, state.keyInfo)
	// 透传原始请求路径与方法给插件。buildForwardHeaders 只克隆了 c.Request.Header，
	// 插件收到的 sdk.ForwardRequest 里没有 Method / URL 字段，光凭 header + body
	// 反推路径非常 fragile（比如 GET /v1/models 和空 body 的 POST 无法区分）。
	// 这里显式把路径和方法塞进头里，插件侧 extractForwardedPath 会优先读取。
	headers.Set("X-Forwarded-Path", state.requestPath)
	headers.Set("X-Forwarded-Method", c.Request.Method)

	fwdReq := &sdk.ForwardRequest{
		Account: buildSDKAccount(state.account),
		Body:    state.body,
		Headers: headers,
		Model:   state.model,
		Stream:  state.stream,
	}
	if state.stream {
		fwdReq.Writer = c.Writer
	}
	return fwdReq
}

func buildForwardHeaders(source http.Header, keyInfo *auth.APIKeyInfo) http.Header {
	headers := source.Clone()
	if keyInfo.GroupServiceTier != "" {
		headers.Set("X-Airgate-Service-Tier", keyInfo.GroupServiceTier)
	}
	if keyInfo.GroupForceInstructions != "" {
		headers.Set("X-Airgate-Force-Instructions", keyInfo.GroupForceInstructions)
	}
	return headers
}

func buildSDKAccount(account *ent.Account) *sdk.Account {
	return &sdk.Account{
		ID:          int64(account.ID),
		Name:        account.Name,
		Platform:    account.Platform,
		Type:        account.Type,
		Credentials: account.Credentials,
		ProxyURL:    buildProxyURL(account),
	}
}

func buildProxyURL(account *ent.Account) string {
	proxy, err := account.Edges.ProxyOrErr()
	if err != nil || proxy == nil {
		return ""
	}

	if proxy.Username != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%d", proxy.Protocol, proxy.Username, proxy.Password, proxy.Address, proxy.Port)
	}
	return fmt.Sprintf("%s://%s:%d", proxy.Protocol, proxy.Address, proxy.Port)
}
