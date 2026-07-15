package plugin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/forwardpath"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func (f *Forwarder) recordAPIRequestError(c *gin.Context, state *forwardState, status int, code, message string) {
	if state == nil {
		return
	}
	f.recordAPIRequestErrorForKey(c, state.keyInfo, state.requestedPlatform, state.requestPath, state.model, status, code, message)
}

func (f *Forwarder) recordAPIRequestErrorForKey(c *gin.Context, keyInfo *auth.APIKeyInfo, platform, path, model string, status int, code, message string) {
	if f == nil || f.requestMonitor == nil || keyInfo == nil {
		return
	}
	method := ""
	ctx := context.Background()
	if c != nil && c.Request != nil {
		method = c.Request.Method
		ctx = c.Request.Context()
	}
	userID := keyInfo.UserID
	groupID := keyInfo.GroupID
	apiKeyID := keyInfo.KeyID
	detail := map[string]interface{}{
		"platform":         platform,
		"model":            model,
		"http_status":      status,
		"http_error_class": httpErrorClassForStatus(status),
		"error_code":       code,
	}
	if attempts := forwardAttemptsFromGinContext(c); attempts > 0 {
		detail["attempts"] = attempts
		detail["retry_count"] = retryCountForAttempts(attempts)
	}
	attachKeyInfoDetail(detail, keyInfo)
	markRequestTraceError(c, "api_request", status, httpErrorClassForStatus(status), code, message)
	input := requestmonitoring.EventInput{
		Type:               requestmonitoring.TypeAPIRequestError,
		Severity:           requestSeverityForStatus(status),
		Source:             requestmonitoring.SourceForwarder,
		RequestID:          middleware.RequestIDFromGinContext(c),
		APIKeyID:           &apiKeyID,
		APIKeyNameSnapshot: keyInfo.KeyName,
		UserID:             &userID,
		UserEmailSnapshot:  keyInfo.UserEmail,
		GroupID:            &groupID,
		Platform:           platform,
		Method:             method,
		Endpoint:           forwardpath.Normalize(path),
		RequestPath:        path,
		Model:              model,
		HTTPStatus:         intPtr(status),
		ErrorCode:          code,
		Title:              "API request error",
		Message:            message,
		Detail:             detail,
	}
	f.recordRequestEvent(ctx, input, requestTraceFromGinContext(c), true)
}

func (f *Forwarder) recordAllRoutesAccountUnavailable(c *gin.Context, state *forwardState, summary allRoutesFailureSummary, response allRoutesFailureResponse, attempts int) {
	if f == nil || f.monitor == nil || state == nil {
		return
	}
	if response.code != "all_routes_account_unavailable" && response.code != "no_available_account" {
		return
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = context.WithoutCancel(c.Request.Context())
	}
	platform := state.requestedPlatform
	pluginID := ""
	if state.plugin != nil {
		pluginID = state.plugin.Name
		if platform == "" {
			platform = state.plugin.Platform
		}
	}
	subjectID := platform
	detail := map[string]interface{}{
		"attempts":            attempts,
		"retry_count":         retryCountForAttempts(attempts),
		"error_code":          response.code,
		"http_status":         response.status,
		"platform":            platform,
		"plugin_id":           pluginID,
		"model":               state.modelForScheduling(),
		"client_model":        state.model,
		"request_path":        state.requestPath,
		"account_unavailable": summary.accountUnavailable,
		"account_dead_seen":   summary.accountDeadSeen,
	}
	if requestID := middleware.RequestIDFromGinContext(c); requestID != "" {
		detail["request_id"] = requestID
	}
	if state.keyInfo != nil {
		if state.keyInfo.GroupID > 0 {
			subjectID = strconv.Itoa(state.keyInfo.GroupID)
			detail["group_id"] = state.keyInfo.GroupID
		}
		if state.keyInfo.GroupName != "" {
			detail["group_name"] = state.keyInfo.GroupName
		}
		if state.keyInfo.UserID > 0 {
			detail["user_id"] = state.keyInfo.UserID
		}
		if state.keyInfo.KeyID > 0 {
			detail["api_key_id"] = state.keyInfo.KeyID
		}
	}
	f.monitor.Record(ctx, monitoring.EventInput{
		Type:        monitoring.TypeSchedulerError,
		Severity:    monitoring.SeverityError,
		Source:      monitoring.SourceForwarder,
		SubjectType: monitoring.SubjectScheduler,
		SubjectID:   subjectID,
		Platform:    platform,
		PluginID:    pluginID,
		ErrorCode:   response.code,
		Title:       "All upstream accounts unavailable",
		Message:     response.message,
		Detail:      detail,
	})
}

func (f *Forwarder) recordMonitorRecoverySuccess(ctx context.Context, state *forwardState) {
	if f == nil || f.monitor == nil || state == nil {
		return
	}
	recorder, ok := any(f.monitor).(monitoring.RecoveryRecorder)
	if !ok {
		return
	}
	platform := state.requestedPlatform
	pluginID := ""
	if state.plugin != nil {
		pluginID = state.plugin.Name
		if platform == "" {
			platform = state.plugin.Platform
		}
	}
	groupID := 0
	if state.keyInfo != nil {
		groupID = state.keyInfo.GroupID
	}
	recorder.RecordRecoverySuccess(ctx, monitoring.RecoverySuccess{
		Type:        monitoring.TypeSchedulerError,
		SubjectType: monitoring.SubjectScheduler,
		Platform:    platform,
		PluginID:    pluginID,
		GroupID:     groupID,
		Model:       state.modelForScheduling(),
	})
}

func (f *Forwarder) recordPluginRouteError(c *gin.Context, keyInfo *auth.APIKeyInfo, platform, path, code, message string) {
	if f == nil || f.requestMonitor == nil {
		return
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	method := ""
	if c != nil && c.Request != nil {
		method = c.Request.Method
	}
	input := requestmonitoring.EventInput{
		Type:        requestmonitoring.TypePluginRouteError,
		Severity:    requestmonitoring.SeverityWarning,
		Source:      requestmonitoring.SourceForwarder,
		RequestID:   middleware.RequestIDFromGinContext(c),
		Platform:    platform,
		Method:      method,
		Endpoint:    forwardpath.Normalize(path),
		RequestPath: path,
		HTTPStatus:  intPtr(http.StatusServiceUnavailable),
		ErrorCode:   code,
		Title:       "Plugin route error",
		Message:     message,
		Detail: map[string]interface{}{
			"platform": platform,
			"stage":    "plugin_route",
		},
	}
	attachRequestKeyInfo(&input, keyInfo)
	markRequestTraceError(c, "plugin_route", http.StatusServiceUnavailable, "server_error", code, message)
	f.recordRequestEvent(ctx, input, requestTraceFromGinContext(c), true)
}

func (f *Forwarder) recordPluginExecutionRetry(ctx context.Context, state *forwardState, execution forwardExecution, attempts int) {
	if attempts < 1 {
		attempts = 1
	}
	f.recordPluginExecutionEvent(ctx, state, execution, pluginExecutionMonitorEvent{
		typeID:       requestmonitoring.TypePluginForwardRetry,
		severity:     requestmonitoring.SeverityInfo,
		title:        "Plugin forward retry",
		stage:        "plugin_forward_retry",
		attempts:     attempts,
		retryCount:   attempts,
		nextAttempt:  attempts + 1,
		finalFailure: false,
	})
}

func (f *Forwarder) recordPluginExecutionFinalFailure(ctx context.Context, state *forwardState, execution forwardExecution, attempts int) {
	if !shouldRecordPluginExecutionError(execution) {
		return
	}
	if attempts < 1 {
		attempts = 1
	}
	f.recordPluginExecutionEvent(ctx, state, execution, pluginExecutionMonitorEvent{
		typeID:       requestmonitoring.TypePluginForwardError,
		severity:     requestmonitoring.SeverityWarning,
		title:        "Plugin forward error",
		stage:        "plugin_forward",
		attempts:     attempts,
		retryCount:   retryCountForAttempts(attempts),
		finalFailure: true,
	})
}

type pluginExecutionMonitorEvent struct {
	typeID       string
	severity     string
	title        string
	stage        string
	attempts     int
	retryCount   int
	nextAttempt  int
	finalFailure bool
}

func (f *Forwarder) recordPluginExecutionEvent(ctx context.Context, state *forwardState, execution forwardExecution, event pluginExecutionMonitorEvent) {
	if f == nil || f.requestMonitor == nil || state == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	code := "plugin_forward_error"
	if execution.outcome.Kind != sdk.OutcomeUnknown {
		code = execution.outcome.Kind.String()
	}
	if upstreamCode := extractErrorCode(execution.outcome.Upstream.Body); upstreamCode != "" {
		code = upstreamCode
	}
	message := judgmentReason(execution)
	pluginID := ""
	platform := state.requestedPlatform
	if state.plugin != nil {
		pluginID = state.plugin.Name
		if platform == "" {
			platform = state.plugin.Platform
		}
	}
	input := requestmonitoring.EventInput{
		Type:           event.typeID,
		Severity:       event.severity,
		Source:         requestmonitoring.SourceForwarder,
		RequestID:      sdk.RequestIDFromContext(ctx),
		Platform:       platform,
		PluginID:       pluginID,
		Method:         "POST",
		Endpoint:       forwardpath.Normalize(state.requestPath),
		RequestPath:    state.requestPath,
		Model:          state.model,
		UpstreamStatus: intPtr(execution.outcome.Upstream.StatusCode),
		ErrorCode:      code,
		Title:          event.title,
		Message:        message,
		Detail: map[string]interface{}{
			"outcome_kind":    execution.outcome.Kind.String(),
			"duration_ms":     execution.duration.Milliseconds(),
			"upstream_status": execution.outcome.Upstream.StatusCode,
			"reason":          message,
			"stage":           event.stage,
			"attempts":        event.attempts,
			"retry_count":     event.retryCount,
			"final_failure":   event.finalFailure,
		},
	}
	if event.nextAttempt > 0 {
		input.Detail["next_attempt"] = event.nextAttempt
	}
	if upstreamRequestID := upstreamRequestIDFromHeaders(execution.outcome.Upstream.Headers); upstreamRequestID != "" {
		input.Detail["upstream_request_id"] = upstreamRequestID
	}
	if state.keyInfo != nil {
		attachRequestKeyInfo(&input, state.keyInfo)
	}
	if state.account != nil {
		accountID := state.account.ID
		input.AccountID = &accountID
		input.AccountNameSnapshot = state.account.Name
	}
	f.recordRequestEvent(ctx, input, state.trace, event.finalFailure)
}

func (f *Forwarder) recordClientRequestError(c *gin.Context, state *forwardState, execution forwardExecution) {
	if f == nil || f.requestMonitor == nil || state == nil || state.keyInfo == nil {
		return
	}
	status := sanitizedClientErrorStatus(execution.outcome)
	message := sanitizedClientErrorMessage(execution.outcome)
	markRequestTraceExecution(c, state, execution)
	errorCode := extractErrorCode(execution.outcome.Upstream.Body)
	if errorCode == "" {
		errorCode = "invalid_request"
	}
	input := requestmonitoring.EventInput{
		Type:           requestmonitoring.TypeClientRequestError,
		Severity:       requestmonitoring.SeverityInfo,
		Source:         requestmonitoring.SourceForwarder,
		RequestID:      middleware.RequestIDFromGinContext(c),
		Platform:       state.requestedPlatform,
		Method:         requestMethod(c, "POST"),
		Endpoint:       forwardpath.Normalize(state.requestPath),
		RequestPath:    state.requestPath,
		Model:          state.model,
		HTTPStatus:     intPtr(status),
		UpstreamStatus: intPtr(execution.outcome.Upstream.StatusCode),
		ErrorCode:      errorCode,
		DurationMS:     execution.duration.Milliseconds(),
		Title:          "Client request error",
		Message:        message,
		Detail: map[string]interface{}{
			"http_error_class": "invalid_request_error",
			"outcome_kind":     execution.outcome.Kind.String(),
			"reason":           execution.outcome.Reason,
			"stage":            "client_request",
		},
	}
	if attempts := forwardAttemptsFromGinContext(c); attempts > 0 {
		input.Detail["attempts"] = attempts
		input.Detail["retry_count"] = retryCountForAttempts(attempts)
	}
	if upstreamRequestID := upstreamRequestIDFromHeaders(execution.outcome.Upstream.Headers); upstreamRequestID != "" {
		input.Detail["upstream_request_id"] = upstreamRequestID
	}
	if state.plugin != nil {
		input.PluginID = state.plugin.Name
		if input.Platform == "" {
			input.Platform = state.plugin.Platform
		}
	}
	attachRequestKeyInfo(&input, state.keyInfo)
	if state.account != nil {
		accountID := state.account.ID
		input.AccountID = &accountID
		input.AccountNameSnapshot = state.account.Name
	}
	recordCtx := context.Background()
	if c != nil && c.Request != nil {
		recordCtx = c.Request.Context()
	}
	f.recordRequestEvent(recordCtx, input, state.trace, true)
}

func (f *Forwarder) recordClientClosedRequest(c *gin.Context, state *forwardState, status int, attempts int) {
	if f == nil || f.requestMonitor == nil || state == nil || state.keyInfo == nil || status == 0 {
		return
	}
	input := requestmonitoring.EventInput{
		Type:        requestmonitoring.TypeClientClosed,
		Severity:    requestmonitoring.SeverityInfo,
		Source:      requestmonitoring.SourceForwarder,
		RequestID:   middleware.RequestIDFromGinContext(c),
		Platform:    state.requestedPlatform,
		Method:      requestMethod(c, "POST"),
		Endpoint:    forwardpath.Normalize(state.requestPath),
		RequestPath: state.requestPath,
		Model:       state.model,
		HTTPStatus:  intPtr(status),
		ErrorCode:   "client_closed_request",
		DurationMS:  timeSinceMilliseconds(state.startedAt),
		Title:       "Client closed request",
		Message:     "Client disconnected before the request completed",
		Detail: map[string]interface{}{
			"attempts":    attempts,
			"retry_count": retryCountForAttempts(attempts),
			"stage":       "client_closed",
		},
	}
	if state.plugin != nil {
		input.PluginID = state.plugin.Name
		if input.Platform == "" {
			input.Platform = state.plugin.Platform
		}
	}
	attachRequestKeyInfo(&input, state.keyInfo)
	if state.account != nil {
		accountID := state.account.ID
		input.AccountID = &accountID
		input.AccountNameSnapshot = state.account.Name
	}
	recordCtx := context.Background()
	if c != nil && c.Request != nil {
		recordCtx = context.WithoutCancel(c.Request.Context())
	}
	markRequestTraceError(c, "client_closed", status, "client_error", "client_closed_request", input.Message)
	f.recordRequestEvent(recordCtx, input, state.trace, true)
}

func attachRequestKeyInfo(input *requestmonitoring.EventInput, keyInfo *auth.APIKeyInfo) {
	if input == nil || keyInfo == nil {
		return
	}
	apiKeyID := keyInfo.KeyID
	userID := keyInfo.UserID
	groupID := keyInfo.GroupID
	input.APIKeyID = &apiKeyID
	input.APIKeyNameSnapshot = keyInfo.KeyName
	input.UserID = &userID
	input.UserEmailSnapshot = keyInfo.UserEmail
	input.GroupID = &groupID
	if input.Detail == nil {
		input.Detail = map[string]interface{}{}
	}
	attachKeyInfoDetail(input.Detail, keyInfo)
}

func attachKeyInfoDetail(detail map[string]interface{}, keyInfo *auth.APIKeyInfo) {
	if detail == nil || keyInfo == nil {
		return
	}
	if keyInfo.KeyID > 0 {
		detail["api_key_id"] = keyInfo.KeyID
	}
	if keyInfo.KeyName != "" {
		detail["api_key_name"] = keyInfo.KeyName
	}
	if keyInfo.UserID > 0 {
		detail["user_id"] = keyInfo.UserID
	}
	if keyInfo.UserEmail != "" {
		detail["user_email"] = keyInfo.UserEmail
	}
	if keyInfo.GroupID > 0 {
		detail["group_id"] = keyInfo.GroupID
	}
	if keyInfo.GroupName != "" {
		detail["group_name"] = keyInfo.GroupName
	}
}

func requestMethod(c *gin.Context, fallback string) string {
	if c == nil || c.Request == nil || c.Request.Method == "" {
		return fallback
	}
	return c.Request.Method
}

func forwardAttemptsFromGinContext(c *gin.Context) int {
	if c == nil {
		return 0
	}
	value, ok := c.Get(ginCtxKeyAttempts)
	if !ok {
		return 0
	}
	switch attempts := value.(type) {
	case int:
		if attempts > 0 {
			return attempts
		}
	case int64:
		if attempts > 0 {
			return int(attempts)
		}
	}
	return 0
}

func retryCountForAttempts(attempts int) int {
	if attempts <= 1 {
		return 0
	}
	return attempts - 1
}

func timeSinceMilliseconds(startedAt time.Time) int64 {
	if startedAt.IsZero() {
		return 0
	}
	return time.Since(startedAt).Milliseconds()
}

func shouldRecordPluginExecutionError(execution forwardExecution) bool {
	if execution.err != nil {
		return true
	}
	switch execution.outcome.Kind {
	case sdk.OutcomeSuccess, sdk.OutcomeClientError:
		return false
	default:
		return true
	}
}

func httpErrorClassForStatus(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= 500:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

func requestSeverityForStatus(status int) string {
	if status >= http.StatusInternalServerError || status == http.StatusTooManyRequests {
		return requestmonitoring.SeverityWarning
	}
	return requestmonitoring.SeverityInfo
}

func upstreamRequestIDFromHeaders(headers http.Header) string {
	if headers == nil {
		return ""
	}
	for _, name := range []string{"x-request-id", "request-id", "openai-request-id", "x-openai-request-id"} {
		if value := headers.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
