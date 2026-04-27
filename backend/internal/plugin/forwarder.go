package plugin

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	"github.com/DouDOU-start/airgate-core/internal/routing"
	"github.com/DouDOU-start/airgate-core/internal/scheduler"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
	sdk "github.com/DouDOU-start/airgate-sdk"
)

// 访问日志富化键的本地别名，避免在大量 c.Set 处重复写出包名。
const (
	ginCtxKeyModel     = middleware.CtxKeyAccessModel
	ginCtxKeyPlatform  = middleware.CtxKeyAccessPlatform
	ginCtxKeyAccountID = middleware.CtxKeyAccessAccountID
	ginCtxKeyAttempts  = middleware.CtxKeyAccessAttempts
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

// allRoutesFailedDefaultRetryAfter 客户端最终被拒时，若没有任何上游 RetryAfter 可参考
// （比如 max_concurrency 打满、所有账号都在冷却但 state_until 没回填到这一层），
// 给客户端一个保守的退避建议。1s 既能避免雪崩，又比 60s 更贴合"瞬时打满"的真实恢复节奏。
const allRoutesFailedDefaultRetryAfter = time.Second

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

	// 请求级 logger：继承 middleware 注入的 request_id / user_id / group_id 等字段，
	// 再叠加 model / platform 让所有 forward 阶段日志自带上下文。
	logger := sdk.LoggerFromContext(c.Request.Context()).With(
		sdk.LogFieldModel, state.model,
		sdk.LogFieldPlatform, state.plugin.Name,
	)
	// http_request 中间件最终会输出一行总览，model/platform 写回 gin ctx 让那一行带上。
	c.Set(ginCtxKeyModel, state.model)
	c.Set(ginCtxKeyPlatform, state.plugin.Name)

	logger.Debug("forward_request_start",
		"stream", state.stream,
		"input_tokens_est", len(state.body),
	)

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

	routes := routesForAPIKey(state, routing.Requirements{
		NeedsImage: requestNeedsImage(state.requestPath, state.model),
	})
	if len(routes) == 0 {
		logger.Warn("forward_no_eligible_route",
			sdk.LogFieldUserID, state.keyInfo.UserID,
		)
		openAIError(c, http.StatusServiceUnavailable, "server_error", "no_available_route", "请求暂时无法完成，请稍后重试")
		return
	}

	var hardExclude []int
	var mwBag map[string]string
	beginCalled := false
	ctx := c.Request.Context()
	startedAt := state.startedAt
	totalAttempts := 0

	// rateLimited* 跟踪本次请求最近一次被上游限流时的退避建议。最终走到 all_routes_failed
	// 时用来给客户端回 429 + Retry-After，而不是无信号的 503，让 SDK 能正确退避。
	// 多次命中限流时取最小值（最早恢复的账号决定何时重试最合理）。
	rateLimitedSeen := false
	rateLimitedRetryAfter := time.Duration(0)

	for _, route := range routes {
		state.selectedRoute = route
		state.keyInfo = keyInfoForRoute(state.keyInfo, route)

		softExclude := []int(nil)
		attempt := 0
		queueDeadline := time.Now().Add(queueWaitTimeout)

		for attempt < maxFailoverAttempts {
			exclude := make([]int, 0, len(hardExclude)+len(softExclude))
			exclude = append(exclude, hardExclude...)
			exclude = append(exclude, softExclude...)

			if err := f.pickAccount(c, state, exclude...); err != nil {
				if len(softExclude) > 0 && time.Now().Before(queueDeadline) {
					softExclude = nil
					select {
					case <-ctx.Done():
						return
					case <-time.After(queuePollInterval):
					}
					continue
				}
				attrs := []any{sdk.LogFieldError, err}
				if len(hardExclude) > 0 {
					attrs = append(attrs, "hard_excluded", hardExclude)
				}
				if len(softExclude) > 0 {
					attrs = append(attrs, "soft_excluded", softExclude)
				}
				logger.Warn("forward_pick_account_failed", attrs...)
				break
			}

			accountID := state.account.ID
			// logger 已经从 auth middleware 继承了 group_id，这里只补 account_id 避免重复字段。
			attemptLogger := logger.With(sdk.LogFieldAccountID, accountID)
			releaseAccountSlot, ok := f.acquireAccountSlot(c, state)
			if !ok {
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
			totalAttempts++

			if f.canFailover(c, state, execution) {
				attrs := []any{
					"attempt", attempt,
					"kind", execution.outcome.Kind,
					sdk.LogFieldDurationMs, execution.duration.Milliseconds(),
					sdk.LogFieldReason, judgmentReason(execution),
				}
				if s := execution.outcome.Upstream.StatusCode; s > 0 {
					attrs = append(attrs, "upstream_status", s)
				}
				if execution.err != nil {
					attrs = append(attrs, sdk.LogFieldError, execution.err)
				}
				attemptLogger.Warn("forward_attempt_failed", attrs...)
				releaseAccountSlot()
				f.applyOutcome(ctx, state, execution)

				if execution.outcome.Kind == sdk.OutcomeAccountRateLimited {
					ra := execution.outcome.RetryAfter
					if !rateLimitedSeen || (ra > 0 && (rateLimitedRetryAfter == 0 || ra < rateLimitedRetryAfter)) {
						rateLimitedSeen = true
						rateLimitedRetryAfter = ra
					}
				}

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
			// 总览写回 gin ctx，由 http_request 中间件统一输出，避免双行重复。
			c.Set(ginCtxKeyAccountID, accountID)
			c.Set(ginCtxKeyAttempts, totalAttempts)
			// 仅在发生过 failover 时单独打 Info；正常一次成功只留 Debug，避免噪声。
			if totalAttempts > 1 {
				attemptLogger.Info("forward_request_completed_after_retry",
					sdk.LogFieldStatus, execution.outcome.Upstream.StatusCode,
					sdk.LogFieldDurationMs, time.Since(startedAt).Milliseconds(),
					"attempts", totalAttempts,
				)
			} else {
				attemptLogger.Debug("forward_request_completed",
					sdk.LogFieldStatus, execution.outcome.Upstream.StatusCode,
					sdk.LogFieldDurationMs, time.Since(startedAt).Milliseconds(),
				)
			}
			return
		}

		logger.Debug("forward_route_failover_exhausted",
			"attempts", attempt,
		)
	}

	failAttrs := []any{
		sdk.LogFieldDurationMs, time.Since(startedAt).Milliseconds(),
		"attempts", totalAttempts,
	}
	if len(hardExclude) > 0 {
		failAttrs = append(failAttrs, "tried_accounts", hardExclude)
	}
	if rateLimitedSeen {
		failAttrs = append(failAttrs, "rate_limited_retry_after_ms", rateLimitedRetryAfter.Milliseconds())
	}
	logger.Error("forward_request_failed", failAttrs...)

	// 走到这里都是"上游容量不足"——上游 429、家族冷却中、并发槽满 + 排队超时，
	// 客户端视角统一归为可重试的限流，回 429 + Retry-After 让 SDK 自动退避。
	// 真正的"无候选分组 / 配置错"已经在最前面 routes 为空时回了 no_available_route，
	// 不会走到这里；这里再回 503 只会让客户端拿到无信号的失败，触发更猛的重试。
	retryAfter := rateLimitedRetryAfter
	if retryAfter <= 0 {
		retryAfter = allRoutesFailedDefaultRetryAfter
	}
	openAIRateLimitError(c, http.StatusTooManyRequests, "all_routes_failed",
		"上游容量暂时不足，请稍后重试", retryAfter)
}

func routesForAPIKey(state *forwardState, requirements routing.Requirements) []routing.Candidate {
	if state == nil || state.keyInfo == nil {
		return nil
	}
	if !apiKeyGroupMatchesRequirements(state.keyInfo, requirements) {
		return nil
	}
	return []routing.Candidate{keyInfoRoute(state.keyInfo)}
}

func apiKeyGroupMatchesRequirements(keyInfo *auth.APIKeyInfo, requirements routing.Requirements) bool {
	if keyInfo == nil {
		return false
	}
	if requirements.NeedsImage && strings.EqualFold(keyInfo.GroupPlatform, "openai") {
		return pluginSettingEnabledForKey(keyInfo.GroupPluginSettings, "openai", "image_enabled")
	}
	return true
}

func pluginSettingEnabledForKey(settings map[string]map[string]string, plugin, key string) bool {
	for pluginName, kv := range settings {
		if !strings.EqualFold(pluginName, plugin) {
			continue
		}
		for k, v := range kv {
			if strings.EqualFold(k, key) {
				return strings.EqualFold(strings.TrimSpace(v), "true")
			}
		}
	}
	return false
}

func keyInfoRoute(keyInfo *auth.APIKeyInfo) routing.Candidate {
	return routing.Candidate{
		GroupID:                keyInfo.GroupID,
		Platform:               keyInfo.GroupPlatform,
		EffectiveRate:          billing.ResolveBillingRateForGroup(keyInfo.UserGroupRates, keyInfo.GroupID, keyInfo.GroupRateMultiplier),
		GroupRateMultiplier:    keyInfo.GroupRateMultiplier,
		GroupServiceTier:       keyInfo.GroupServiceTier,
		GroupForceInstructions: keyInfo.GroupForceInstructions,
		GroupPluginSettings:    clonePluginSettingsForKey(keyInfo.GroupPluginSettings),
	}
}

func clonePluginSettingsForKey(in map[string]map[string]string) map[string]map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(in))
	for plugin, settings := range in {
		if len(settings) == 0 {
			continue
		}
		out[plugin] = make(map[string]string, len(settings))
		for k, v := range settings {
			out[plugin][k] = v
		}
	}
	return out
}

func keyInfoForRoute(base *auth.APIKeyInfo, route routing.Candidate) *auth.APIKeyInfo {
	info := *base
	info.GroupID = route.GroupID
	info.GroupPlatform = route.Platform
	info.GroupRateMultiplier = route.GroupRateMultiplier
	info.GroupServiceTier = route.GroupServiceTier
	info.GroupForceInstructions = route.GroupForceInstructions
	info.GroupPluginSettings = route.GroupPluginSettings
	return &info
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
