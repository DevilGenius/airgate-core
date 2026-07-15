package plugin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/forwardpath"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

const ginCtxKeyRequestTrace = "airgate_request_trace"

type requestTraceSession struct {
	startedAt time.Time

	requestID string
	method    string
	path      string
	platform  string
	pluginID  string
	model     string
	stream    bool

	requestHeaders http.Header
	requestBody    []byte

	previousResponseID          string
	requireContinuationAffinity bool
	continuationRecoveryApplied bool

	apiKeyID            *int
	apiKeyNameSnapshot  string
	userID              *int
	userEmailSnapshot   string
	groupID             *int
	accountID           *int
	accountNameSnapshot string

	attempts []requestmonitoring.TraceAttempt
	final    requestmonitoring.TraceFinalError
	finalSet bool

	eventHandled bool
}

func (f *Forwarder) beginRequestTrace(c *gin.Context) *requestTraceSession {
	if f == nil || !f.RequestTraceEnabled() || f.requestMonitor == nil || c == nil {
		return nil
	}
	if _, ok := f.requestMonitor.(requestmonitoring.TraceRecorder); !ok {
		return nil
	}
	session := &requestTraceSession{
		startedAt: time.Now(),
		requestID: middleware.RequestIDFromGinContext(c),
		method:    requestMethod(c, http.MethodPost),
		path:      requestPath(c),
	}
	if c.Request != nil {
		session.requestHeaders = safeRequestTraceHeaders(c.Request.Header)
	}
	c.Set(ginCtxKeyRequestTrace, session)
	return session
}

func requestTraceFromGinContext(c *gin.Context) *requestTraceSession {
	if c == nil {
		return nil
	}
	value, ok := c.Get(ginCtxKeyRequestTrace)
	if !ok {
		return nil
	}
	session, _ := value.(*requestTraceSession)
	return session
}

func (s *requestTraceSession) captureRequestBody(body []byte, contentType string) {
	if s == nil {
		return
	}
	s.requestBody = body
	if contentType != "" {
		if s.requestHeaders == nil {
			s.requestHeaders = make(http.Header)
		}
		s.requestHeaders.Set("Content-Type", contentType)
	}
}

func (s *requestTraceSession) bindKeyInfo(keyInfo *auth.APIKeyInfo) {
	if s == nil || keyInfo == nil {
		return
	}
	apiKeyID := keyInfo.KeyID
	userID := keyInfo.UserID
	groupID := keyInfo.GroupID
	s.apiKeyID = &apiKeyID
	s.apiKeyNameSnapshot = keyInfo.KeyName
	s.userID = &userID
	s.userEmailSnapshot = keyInfo.UserEmail
	s.groupID = &groupID
}

func (s *requestTraceSession) captureParsedRequest(parsed parsedRequest) {
	if s == nil {
		return
	}
	s.model = parsed.Model
	s.stream = parsed.Stream
	s.previousResponseID = parsed.PreviousResponseID
	s.requireContinuationAffinity = requestRequiresContinuationAffinity(parsed)
}

func (s *requestTraceSession) bindState(state *forwardState) {
	if s == nil || state == nil {
		return
	}
	s.path = state.requestPath
	s.platform = state.requestedPlatform
	s.model = state.model
	s.stream = state.stream
	s.previousResponseID = state.previousResponseID
	s.requireContinuationAffinity = state.requireContinuationAffinity
	s.continuationRecoveryApplied = state.continuationRecoveryApplied
	if state.plugin != nil {
		s.pluginID = state.plugin.Name
		if s.platform == "" {
			s.platform = state.plugin.Platform
		}
	}
	if state.keyInfo != nil {
		s.bindKeyInfo(state.keyInfo)
	}
}

func (s *requestTraceSession) addFailedAttempt(number int, state *forwardState, execution forwardExecution) {
	if s == nil || (execution.err == nil && execution.outcome.Kind == sdk.OutcomeSuccess) {
		return
	}
	attempt := requestmonitoring.TraceAttempt{
		Number:          number,
		OutcomeKind:     execution.outcome.Kind.String(),
		FailoverScope:   string(execution.outcome.FailoverScope),
		Reason:          judgmentReason(execution),
		DurationMS:      execution.duration.Milliseconds(),
		UpstreamStatus:  execution.outcome.Upstream.StatusCode,
		UpstreamHeaders: execution.outcome.Upstream.Headers,
		UpstreamBody:    execution.outcome.Upstream.Body,
	}
	if execution.err != nil {
		attempt.PluginError = execution.err.Error()
	}
	if state != nil {
		s.continuationRecoveryApplied = s.continuationRecoveryApplied || state.continuationRecoveryApplied
		attempt.ClientModel = state.dispatchPlan.ClientModel
		attempt.SchedulingModel = state.dispatchPlan.SchedulingModel
		attempt.WireModel = state.dispatchPlan.WireModel
		attempt.RuleID = state.dispatchPlan.RuleID
		attempt.Operation = state.dispatchPlan.Operation
		attempt.TimeoutProfile = state.dispatchPlan.TimeoutProfile
		if state.account != nil {
			attempt.AccountID = state.account.ID
			attempt.AccountName = state.account.Name
			attempt.AccountType = state.account.Type
			accountID := state.account.ID
			s.accountID = &accountID
			s.accountNameSnapshot = state.account.Name
		}
	}
	if diagnostic := execution.outcome.FinalErrorDiagnostic; diagnostic != nil {
		attempt.UpstreamErrorBody = diagnostic.UpstreamErrorBody
		if len(diagnostic.OutboundRequests) > 0 {
			attempt.OutboundRequests = make([]requestmonitoring.TraceOutboundRequest, 0, len(diagnostic.OutboundRequests))
			for _, request := range diagnostic.OutboundRequests {
				attempt.OutboundRequests = append(attempt.OutboundRequests, requestmonitoring.TraceOutboundRequest{
					Transport:           request.Transport,
					Method:              request.Method,
					URL:                 request.URL,
					Headers:             request.Headers,
					Body:                request.Body,
					StatusCode:          request.StatusCode,
					BodyRedacted:        request.BodyRedacted,
					BodyRedactionReason: request.BodyRedactionReason,
					BodyOriginalSize:    request.BodyOriginalSize,
				})
			}
		}
	}
	s.attempts = append(s.attempts, attempt)
}

func (s *requestTraceSession) markFinal(stage string, status int, errType, code, message string) {
	if s == nil || s.finalSet {
		return
	}
	s.final = requestmonitoring.TraceFinalError{
		Stage:      stage,
		HTTPStatus: status,
		ErrorType:  errType,
		ErrorCode:  code,
		Message:    message,
	}
	s.finalSet = true
}

func markRequestTraceError(c *gin.Context, stage string, status int, errType, code, message string) {
	if trace := requestTraceFromGinContext(c); trace != nil {
		trace.markFinal(stage, status, errType, code, message)
	}
}

func markRequestTraceExecution(c *gin.Context, state *forwardState, execution forwardExecution) {
	trace := requestTraceFromGinContext(c)
	if trace == nil && state != nil {
		trace = state.trace
	}
	if trace == nil {
		return
	}
	status := http.StatusBadGateway
	errType := "server_error"
	code := execution.outcome.Kind.String()
	message := judgmentReason(execution)
	switch execution.outcome.Kind {
	case sdk.OutcomeClientError:
		status = sanitizedClientErrorStatus(execution.outcome)
		errType = "invalid_request_error"
		if upstreamCode := extractErrorCode(execution.outcome.Upstream.Body); upstreamCode != "" {
			code = upstreamCode
		} else {
			code = "invalid_request"
		}
		if upstreamMessage := extractErrorMessage(execution.outcome.Upstream.Body); upstreamMessage != "" {
			message = upstreamMessage
		}
	case sdk.OutcomeAccountRateLimited:
		status = http.StatusTooManyRequests
		errType = "rate_limit_error"
		code = "upstream_rate_limit"
	case sdk.OutcomeAccountUnavailable:
		status = http.StatusTooManyRequests
		errType = "rate_limit_error"
		code = "upstream_account_unavailable"
	case sdk.OutcomeStreamAborted:
		code = "stream_aborted"
	case sdk.OutcomeUnknown:
		code = "plugin_forward_error"
	}
	trace.markFinal("plugin_forward", status, errType, code, message)
}

func (s *requestTraceSession) traceInput() requestmonitoring.TraceInput {
	return requestmonitoring.TraceInput{
		ObservedAt:                  time.Now(),
		Method:                      s.method,
		Path:                        s.path,
		Platform:                    s.platform,
		PluginID:                    s.pluginID,
		Model:                       s.model,
		Stream:                      s.stream,
		RequestHeaders:              s.requestHeaders,
		RequestBody:                 s.requestBody,
		PreviousResponseID:          s.previousResponseID,
		RequireContinuationAffinity: s.requireContinuationAffinity,
		ContinuationRecoveryApplied: s.continuationRecoveryApplied,
		Attempts:                    s.attempts,
		Final:                       s.final,
	}
}

func (s *requestTraceSession) enrichFromEvent(input requestmonitoring.EventInput) {
	if s == nil {
		return
	}
	if s.requestID == "" {
		s.requestID = input.RequestID
	}
	if s.platform == "" {
		s.platform = input.Platform
	}
	if s.pluginID == "" {
		s.pluginID = input.PluginID
	}
	if s.model == "" {
		s.model = input.Model
	}
	if s.path == "" {
		s.path = input.RequestPath
	}
}

func (s *requestTraceSession) genericEventInput() requestmonitoring.EventInput {
	status := s.final.HTTPStatus
	code := s.final.ErrorCode
	if code == "" {
		code = "request_failed"
	}
	message := s.final.Message
	if message == "" {
		message = "request ended with an error"
	}
	detail := map[string]interface{}{
		"stage":         s.final.Stage,
		"final_failure": true,
		"attempts":      len(s.attempts),
		"retry_count":   retryCountForAttempts(len(s.attempts)),
		"trace_enabled": true,
	}
	var upstreamStatus *int
	if len(s.attempts) > 0 {
		last := s.attempts[len(s.attempts)-1]
		upstreamStatus = intPtr(last.UpstreamStatus)
		if upstreamRequestID := upstreamRequestIDFromHeaders(last.UpstreamHeaders); upstreamRequestID != "" {
			detail["upstream_request_id"] = upstreamRequestID
		}
	}
	return requestmonitoring.EventInput{
		Type:                requestmonitoring.TypeAPIRequestError,
		Severity:            requestSeverityForStatus(status),
		Source:              requestmonitoring.SourceForwarder,
		RequestID:           s.requestID,
		APIKeyID:            s.apiKeyID,
		APIKeyNameSnapshot:  s.apiKeyNameSnapshot,
		UserID:              s.userID,
		UserEmailSnapshot:   s.userEmailSnapshot,
		GroupID:             s.groupID,
		AccountID:           s.accountID,
		AccountNameSnapshot: s.accountNameSnapshot,
		Platform:            s.platform,
		PluginID:            s.pluginID,
		Method:              s.method,
		Endpoint:            forwardpath.Normalize(s.path),
		RequestPath:         s.path,
		Model:               s.model,
		HTTPStatus:          intPtr(status),
		UpstreamStatus:      upstreamStatus,
		ErrorCode:           code,
		DurationMS:          timeSinceMilliseconds(s.startedAt),
		Title:               "Final request error",
		Message:             message,
		Detail:              detail,
	}
}

func (f *Forwarder) recordRequestEvent(ctx context.Context, input requestmonitoring.EventInput, trace *requestTraceSession, final bool) {
	if f == nil || f.requestMonitor == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
	}
	if final && trace != nil && trace.finalSet && !trace.eventHandled {
		trace.enrichFromEvent(input)
		if recorder, ok := f.requestMonitor.(requestmonitoring.TraceRecorder); ok {
			recorder.RecordRequestTrace(ctx, input, trace.traceInput())
			trace.eventHandled = true
			return
		}
	}
	f.requestMonitor.RecordRequest(ctx, input)
}

func (f *Forwarder) finishRequestTrace(c *gin.Context, trace *requestTraceSession) {
	if f == nil || trace == nil || trace.eventHandled || f.requestMonitor == nil {
		return
	}
	if !trace.finalSet {
		status := 0
		if c != nil && c.Writer != nil {
			status = c.Writer.Status()
		}
		if status < http.StatusBadRequest {
			return
		}
		trace.markFinal("response", status, httpErrorClassForStatus(status), "request_failed", "request ended with an error")
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = context.WithoutCancel(c.Request.Context())
	}
	f.recordRequestEvent(ctx, trace.genericEventInput(), trace, true)
}

func safeRequestTraceHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return nil
	}
	safe := make(http.Header)
	for name, values := range headers {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "accept", "content-type", "openai-beta", "originator", "user-agent", "x-openai-previous-response-id",
			"session_id", "session-id", "x-session-id", "conversation_id", "conversation-id", "x-codex-turn-state":
			safe[name] = append([]string(nil), values...)
		}
	}
	if len(safe) == 0 {
		return nil
	}
	return safe
}
