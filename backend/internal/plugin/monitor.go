package plugin

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func (f *Forwarder) recordAPIRequestError(c *gin.Context, state *forwardState, status int, code, message, severity string) {
	if state == nil {
		return
	}
	f.recordAPIRequestErrorForKey(c, state.keyInfo, state.requestedPlatform, state.requestPath, state.model, status, code, message, severity)
}

func (f *Forwarder) recordAPIRequestErrorForKey(c *gin.Context, keyInfo *auth.APIKeyInfo, platform, path, model string, status int, code, message string, severity ...string) {
	if f == nil || f.monitor == nil || keyInfo == nil {
		return
	}
	level := monitoring.SeverityWarning
	if len(severity) > 0 && severity[0] != "" {
		level = severity[0]
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
	f.monitor.Record(ctx, monitoring.EventInput{
		Type:               monitoring.TypeAPIRequestError,
		Severity:           level,
		Source:             monitoring.SourceForwarder,
		SubjectType:        monitoring.SubjectAPIKey,
		SubjectID:          strconv.Itoa(apiKeyID),
		APIKeyID:           &apiKeyID,
		APIKeyNameSnapshot: keyInfo.KeyName,
		UserID:             &userID,
		UserEmailSnapshot:  keyInfo.UserEmail,
		GroupID:            &groupID,
		Platform:           platform,
		Method:             method,
		Endpoint:           normalizeForwardPath(path),
		RequestPath:        path,
		Model:              model,
		HTTPStatus:         intPtr(status),
		ErrorCode:          code,
		Title:              "API request error",
		Message:            message,
		Detail: map[string]interface{}{
			"platform":         platform,
			"model":            model,
			"http_status":      status,
			"http_error_class": httpErrorClassForStatus(status),
			"error_code":       code,
		},
	})
}

func (f *Forwarder) recordPluginRouteError(c *gin.Context, keyInfo *auth.APIKeyInfo, platform, path, code, message string) {
	if f == nil || f.monitor == nil {
		return
	}
	f.recordAPIRequestErrorForKey(c, keyInfo, platform, path, "", http.StatusServiceUnavailable, code, message, monitoring.SeverityError)
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	f.monitor.Record(ctx, monitoring.EventInput{
		Type:        monitoring.TypePluginError,
		Severity:    monitoring.SeverityError,
		Source:      monitoring.SourceForwarder,
		SubjectType: monitoring.SubjectPlugin,
		SubjectID:   platform,
		Platform:    platform,
		Endpoint:    normalizeForwardPath(path),
		RequestPath: path,
		HTTPStatus:  intPtr(http.StatusServiceUnavailable),
		ErrorCode:   code,
		Title:       "Plugin route error",
		Message:     message,
		Detail: map[string]interface{}{
			"platform": platform,
			"stage":    "plugin_route",
		},
	})
}

func (f *Forwarder) recordPluginExecutionError(ctx context.Context, state *forwardState, execution forwardExecution) {
	if f == nil || f.monitor == nil || state == nil {
		return
	}
	if !shouldRecordPluginExecutionError(execution) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	code := "plugin_forward_error"
	if execution.outcome.Kind != sdk.OutcomeUnknown {
		code = execution.outcome.Kind.String()
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
	input := monitoring.EventInput{
		Type:           monitoring.TypePluginError,
		Severity:       monitoring.SeverityError,
		Source:         monitoring.SourceForwarder,
		SubjectType:    monitoring.SubjectPlugin,
		SubjectID:      pluginID,
		Platform:       platform,
		PluginID:       pluginID,
		Method:         "POST",
		Endpoint:       normalizeForwardPath(state.requestPath),
		RequestPath:    state.requestPath,
		Model:          state.model,
		UpstreamStatus: intPtr(execution.outcome.Upstream.StatusCode),
		ErrorCode:      code,
		Title:          "Plugin forward error",
		Message:        message,
		Detail: map[string]interface{}{
			"outcome_kind":    execution.outcome.Kind.String(),
			"duration_ms":     execution.duration.Milliseconds(),
			"upstream_status": execution.outcome.Upstream.StatusCode,
			"reason":          message,
			"stage":           "plugin_forward",
		},
	}
	if state.keyInfo != nil {
		apiKeyID := state.keyInfo.KeyID
		userID := state.keyInfo.UserID
		groupID := state.keyInfo.GroupID
		input.APIKeyID = &apiKeyID
		input.APIKeyNameSnapshot = state.keyInfo.KeyName
		input.UserID = &userID
		input.UserEmailSnapshot = state.keyInfo.UserEmail
		input.GroupID = &groupID
	}
	if state.account != nil {
		accountID := state.account.ID
		input.AccountID = &accountID
		input.AccountNameSnapshot = state.account.Name
	}
	f.monitor.Record(ctx, input)
}

func shouldRecordPluginExecutionError(execution forwardExecution) bool {
	if execution.err != nil {
		return true
	}
	switch execution.outcome.Kind {
	case sdk.OutcomeUpstreamTransient, sdk.OutcomeStreamAborted, sdk.OutcomeUnknown:
		return true
	default:
		return false
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

func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
