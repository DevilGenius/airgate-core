package plugin

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/routing"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type captureMonitorRecorder struct {
	events []monitoring.EventInput
}

func (r *captureMonitorRecorder) Record(_ context.Context, input monitoring.EventInput) {
	r.events = append(r.events, input)
}

func (r *captureMonitorRecorder) ResolveBySubject(context.Context, monitoring.ResolveQuery) {}

type captureRequestMonitorRecorder struct {
	events []requestmonitoring.EventInput
}

func (r *captureRequestMonitorRecorder) RecordRequest(_ context.Context, input requestmonitoring.EventInput) {
	r.events = append(r.events, input)
}

func TestParseBody(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-4.1","stream":false,"metadata":{"user_id":"session-123"}}`)

	parsed := parseBody(body, "application/json")
	if parsed.Model != "gpt-4.1" {
		t.Fatalf("Model = %q, want %q", parsed.Model, "gpt-4.1")
	}
	if parsed.SessionID != "session-123" {
		t.Fatalf("SessionID = %q, want %q", parsed.SessionID, "session-123")
	}
	if parsed.Stream {
		t.Fatalf("Stream = true, want false")
	}
}

func TestParseBody_StreamTrue(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-4.1","stream":true,"metadata":{"user_id":"sess-1"}}`)

	parsed := parseBody(body, "application/json")
	if parsed.Model != "gpt-4.1" {
		t.Fatalf("Model = %q, want %q", parsed.Model, "gpt-4.1")
	}
	if !parsed.Stream {
		t.Fatalf("Stream = false, want true")
	}
	if parsed.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want %q", parsed.SessionID, "sess-1")
	}
}

func TestResolveRequestSessionIDFromMetadataUserIDJSON(t *testing.T) {
	t.Parallel()

	parsed := parsedRequest{SessionID: `{"device_id":"device-a","session_id":"session-abc"}`}

	if got := resolveRequestSessionID(nil, parsed); got != "claude:session-abc" {
		t.Fatalf("resolveRequestSessionID() = %q, want claude:session-abc", got)
	}
}

func TestResolveRequestSessionIDMetadataUserIDJSONWithoutSessionFallsBackToHeader(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	headers.Set("Session-Id", "header-session")
	parsed := parsedRequest{SessionID: `{"device_id":"device-a"}`}

	if got := resolveRequestSessionID(headers, parsed); got != "header-session" {
		t.Fatalf("resolveRequestSessionID() = %q, want header-session", got)
	}
}

func TestResolveRequestSessionIDMetadataUserIDJSONWithoutSessionFallsBackToPromptCache(t *testing.T) {
	t.Parallel()

	parsed := parsedRequest{
		SessionID:      `{"device_id":"device-a"}`,
		PromptCacheKey: "pcache-1",
	}

	if got := resolveRequestSessionID(nil, parsed); got != "prompt_cache:pcache-1" {
		t.Fatalf("resolveRequestSessionID() = %q, want prompt_cache:pcache-1", got)
	}
}

func TestResolveRequestSessionIDFromLegacyMetadataUserID(t *testing.T) {
	t.Parallel()

	parsed := parsedRequest{SessionID: "user_xxx_account__session_ac980658-63bd-4fb3-97ba-8da64cb1e344"}

	if got := resolveRequestSessionID(nil, parsed); got != "claude:ac980658-63bd-4fb3-97ba-8da64cb1e344" {
		t.Fatalf("resolveRequestSessionID() = %q, want legacy Claude session", got)
	}
}

func TestResolveRequestSessionIDFromSessionIDHeader(t *testing.T) {
	t.Parallel()

	headers := http.Header{"Session-Id": []string{"header-session"}}

	if got := resolveRequestSessionID(headers, parsedRequest{}); got != "header-session" {
		t.Fatalf("resolveRequestSessionID() = %q, want header-session", got)
	}
}

func TestResolveRequestSessionIDFromBodyConversationID(t *testing.T) {
	t.Parallel()

	parsed := parseBody([]byte(`{"model":"gpt-5.4","conversation_id":"conv-123"}`), "application/json")

	if got := resolveRequestSessionID(nil, parsed); got != "conversation:conv-123" {
		t.Fatalf("resolveRequestSessionID() = %q, want conversation:conv-123", got)
	}
}

func TestParseBodyContinuationSignals(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","prompt_cache_key":"pcache_1","previous_response_id":"resp_1","input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`)
	parsed := parseBody(body, "application/json")
	if parsed.PromptCacheKey != "pcache_1" {
		t.Fatalf("PromptCacheKey = %q, want pcache_1", parsed.PromptCacheKey)
	}
	if parsed.PreviousResponseID != "resp_1" {
		t.Fatalf("PreviousResponseID = %q, want resp_1", parsed.PreviousResponseID)
	}
	if !parsed.HasToolOutput {
		t.Fatalf("HasToolOutput = false, want true")
	}
	if parsed.HasToolCallContext {
		t.Fatalf("HasToolCallContext = true, want false")
	}
	if !requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = false, want true")
	}
}

func TestParseBodyContinuationSignalsWithToolCallContext(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","input":[{"type":"function_call","call_id":"call_1","name":"lookup"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`)
	parsed := parseBody(body, "application/json")
	if !parsed.HasToolOutput {
		t.Fatalf("HasToolOutput = false, want true")
	}
	if !parsed.HasToolCallContext {
		t.Fatalf("HasToolCallContext = false, want true")
	}
	if requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = true, want false")
	}
}

func TestParseBodyPreviousResponseIDOnlyDoesNotRequireContinuationAffinity(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	parsed := parseBody(body, "application/json")
	if parsed.PreviousResponseID != "resp_old" {
		t.Fatalf("PreviousResponseID = %q, want resp_old", parsed.PreviousResponseID)
	}
	if requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = true, want false")
	}
}

func TestParseBodyEncryptedContentDoesNotRequireContinuationAffinity(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","input":[{"type":"reasoning","id":"rs_1","encrypted_content":"sealed"}]}`)
	parsed := parseBody(body, "application/json")
	if !parsed.HasEncryptedContent {
		t.Fatalf("HasEncryptedContent = false, want true")
	}
	if requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = true, want false")
	}
}

func TestParseBodyEncryptedContentSingleObjectDoesNotRequireContinuationAffinity(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","input":{"type":"reasoning","id":"rs_1","encrypted_content":"sealed"}}`)
	parsed := parseBody(body, "application/json")
	if !parsed.HasEncryptedContent {
		t.Fatalf("HasEncryptedContent = false, want true")
	}
	if requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = true, want false")
	}
}

func TestParseBodyEncryptedContentIncludeDoesNotRequireContinuationAffinity(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","include":["reasoning.encrypted_content"],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	parsed := parseBody(body, "application/json")
	if parsed.HasEncryptedContent {
		t.Fatalf("HasEncryptedContent = true, want false")
	}
	if requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = true, want false")
	}
}

func TestParseBodyEncryptedContentNonReasoningItemDoesNotRequireContinuationAffinity(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","input":[{"type":"message","encrypted_content":"extension","content":[{"type":"input_text","text":"hi"}]}]}`)
	parsed := parseBody(body, "application/json")
	if parsed.HasEncryptedContent {
		t.Fatalf("HasEncryptedContent = true, want false")
	}
	if requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = true, want false")
	}
}

func TestParseBodyCompactionReplayDoesNotRequireContinuationAffinity(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":[{"type":"compaction","encrypted_content":"summary"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`)
	parsed := parseBody(body, "application/json")
	if !parsed.HasCompactionReplay {
		t.Fatalf("HasCompactionReplay = false, want true")
	}
	if requestRequiresContinuationAffinity(parsed) {
		t.Fatalf("requestRequiresContinuationAffinity = true, want false")
	}
}

func TestRecoverContinuationAffinityMissingDelegatesPreviousResponseAndEncryptedContent(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":[{"type":"reasoning","id":"rs_1","encrypted_content":"sealed"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	state := &forwardState{
		body:                        originalBody,
		previousResponseID:          "resp_old",
		requireContinuationAffinity: true,
	}

	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissing error: %v", err)
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if state.previousResponseID != "" {
		t.Fatalf("previousResponseID = %q, want empty", state.previousResponseID)
	}
	if state.requireContinuationAffinity {
		t.Fatalf("requireContinuationAffinity = true, want false")
	}
	if string(state.body) != string(originalBody) {
		t.Fatalf("body = %s, want unchanged %s", state.body, originalBody)
	}
}

func TestRecoverContinuationAffinityMissingDelegatesPreviousResponseOnly(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue with full context"}]}]}`)
	state := &forwardState{
		body:               originalBody,
		previousResponseID: "resp_old",
	}

	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissing error: %v", err)
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if state.previousResponseID != "" {
		t.Fatalf("previousResponseID = %q, want empty", state.previousResponseID)
	}
	if string(state.body) != string(originalBody) {
		t.Fatalf("body = %s, want unchanged %s", state.body, originalBody)
	}
}

func TestRecoverContinuationAffinityMissingAllowsModelBudgetFullContext(t *testing.T) {
	t.Parallel()

	largeText := strings.Repeat("x", 2<<20)
	manager := &Manager{
		modelCache: map[string][]sdk.ModelInfo{
			"openai": {
				{ID: "gpt-large", ContextWindow: 1000000},
			},
		},
	}
	state := &forwardState{
		body:               []byte(`{"model":"gpt-large","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"` + largeText + `"}]}]}`),
		model:              "gpt-large",
		requestedPlatform:  "openai",
		previousResponseID: "resp_old",
	}

	recovered, err := recoverContinuationAffinityMissingWithManager(manager, state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissingWithManager error: %v", err)
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if state.previousResponseID != "" {
		t.Fatalf("previousResponseID = %q, want empty", state.previousResponseID)
	}
}

func TestRecoverContinuationAffinityMissingHandlesHeaderOnlyPreviousResponse(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue with full context"}]}]}`)
	state := &forwardState{
		body:               originalBody,
		previousResponseID: "resp_header",
	}

	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissing error: %v", err)
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if state.previousResponseID != "" {
		t.Fatalf("previousResponseID = %q, want empty", state.previousResponseID)
	}
	if !state.continuationRecoveryApplied {
		t.Fatalf("continuationRecoveryApplied = false, want true")
	}
	if string(state.body) != string(originalBody) {
		t.Fatalf("body = %s, want unchanged %s", state.body, originalBody)
	}
}

func TestRecoverContinuationAffinityMissingDelegatesOversizedReplay(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{"model":"gpt-5.4-mini","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"` + strings.Repeat("x", 2<<20) + `"}]}]}`)
	state := &forwardState{
		body:               originalBody,
		previousResponseID: "resp_old",
	}

	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissing error: %v", err)
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if string(state.body) != string(originalBody) {
		t.Fatalf("body = %s, want unchanged", state.body)
	}
}

func TestRecoverContinuationAffinityMissingDelegatesOversizedFullContext(t *testing.T) {
	t.Parallel()

	largeText := strings.Repeat("x", 2<<20)
	originalBody := []byte(`{"model":"gpt-5.4-mini","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"` + largeText + `"}]}]}`)
	state := &forwardState{
		body:               originalBody,
		previousResponseID: "resp_old",
	}

	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissing error: %v", err)
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if state.previousResponseID != "" {
		t.Fatalf("previousResponseID = %q, want empty", state.previousResponseID)
	}
	if string(state.body) != string(originalBody) {
		t.Fatalf("body = %s, want unchanged", state.body)
	}
}

func TestRecoverContinuationAffinityMissingAllowsCompactionReplayToolOutput(t *testing.T) {
	t.Parallel()

	state := &forwardState{
		body:                        []byte(`{"model":"gpt-5.4-mini","previous_response_id":"resp_old","input":[{"type":"compaction","encrypted_content":"summary"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`),
		previousResponseID:          "resp_old",
		requireContinuationAffinity: true,
	}

	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissing error: %v", err)
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if state.previousResponseID != "" {
		t.Fatalf("previousResponseID = %q, want empty", state.previousResponseID)
	}
	if state.requireContinuationAffinity {
		t.Fatalf("requireContinuationAffinity = true, want false")
	}
}

func TestRecoverContinuationAffinityMissingKeepsFunctionCallOutputWithoutContextHard(t *testing.T) {
	t.Parallel()

	state := &forwardState{
		body:                        []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`),
		previousResponseID:          "resp_old",
		requireContinuationAffinity: true,
	}

	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil {
		t.Fatalf("recoverContinuationAffinityMissing error: %v", err)
	}
	if recovered {
		t.Fatalf("recovered = true, want false")
	}
	if state.previousResponseID != "resp_old" {
		t.Fatalf("previousResponseID = %q, want resp_old", state.previousResponseID)
	}
	if !state.requireContinuationAffinity {
		t.Fatalf("requireContinuationAffinity = false, want true")
	}
}

func TestParseBody_MultipartIgnoresFileParts(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("image", "input.png")
	if err != nil {
		t.Fatalf("CreateFormFile error: %v", err)
	}
	if _, err := file.Write(bytes.Repeat([]byte("x"), 1024)); err != nil {
		t.Fatalf("file.Write error: %v", err)
	}
	if err := writer.WriteField("model", " gpt-image-1 "); err != nil {
		t.Fatalf("WriteField(model) error: %v", err)
	}
	if err := writer.WriteField("stream", "true"); err != nil {
		t.Fatalf("WriteField(stream) error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close error: %v", err)
	}

	parsed := parseBody(body.Bytes(), writer.FormDataContentType())
	if parsed.Model != "gpt-image-1" {
		t.Fatalf("Model = %q, want %q", parsed.Model, "gpt-image-1")
	}
	if !parsed.Stream {
		t.Fatalf("Stream = false, want true")
	}
}

func TestParseRequestRejectsGETImageSubmitBeforeScheduling(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/images/generations", nil)
	c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{UserID: 11, KeyID: 22, GroupPlatform: "openai"})

	state, ok := (&Forwarder{}).parseRequest(c)
	if ok {
		t.Fatalf("parseRequest ok = true, want false with state %#v", state)
	}
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if allow := recorder.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", allow)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("body = %s, want method_not_allowed error", recorder.Body.String())
	}
}

func TestParseRequestRejectsImageSubmitWithoutModelBeforeScheduling(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{UserID: 11, KeyID: 22, GroupPlatform: "openai"})

	state, ok := (&Forwarder{}).parseRequest(c)
	if ok {
		t.Fatalf("parseRequest ok = true, want false with state %#v", state)
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"code":"invalid_request"`) || !strings.Contains(body, "model is required") {
		t.Fatalf("body = %s, want invalid_request model error", body)
	}
}

func TestParseRequestRejectsForwardRequestWithoutModelBeforeScheduling(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{UserID: 11, KeyID: 22, GroupPlatform: "openai"})

	state, ok := (&Forwarder{}).parseRequest(c)
	if ok {
		t.Fatalf("parseRequest ok = true, want false with state %#v", state)
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"code":"invalid_request"`) || !strings.Contains(body, "model is required") {
		t.Fatalf("body = %s, want invalid_request model error", body)
	}
}

func TestCanceledRequestStatus(t *testing.T) {
	t.Parallel()

	if got := canceledRequestStatus(context.Canceled); got != statusClientClosedRequest {
		t.Fatalf("canceledRequestStatus(context.Canceled) = %d, want %d", got, statusClientClosedRequest)
	}
	if got := canceledRequestStatus(context.DeadlineExceeded); got != http.StatusGatewayTimeout {
		t.Fatalf("canceledRequestStatus(context.DeadlineExceeded) = %d, want %d", got, http.StatusGatewayTimeout)
	}
	if got := canceledRequestStatus(nil); got != 0 {
		t.Fatalf("canceledRequestStatus(nil) = %d, want 0", got)
	}
}

func TestFinalizeRequestContextUsesBackgroundForCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := finalizeRequestContext(ctx); got.Err() != nil {
		t.Fatalf("finalizeRequestContext(canceled).Err() = %v, want nil", got.Err())
	}
}

func TestHasForwardResult(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		execution forwardExecution
		want      bool
	}{
		{
			name: "success",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess},
			},
			want: true,
		},
		{
			name: "unknown-empty",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUnknown},
			},
			want: false,
		},
		{
			name: "status-only",
			execution: forwardExecution{
				outcome: sdk.ForwardOutcome{
					Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway},
				},
			},
			want: true,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasForwardResult(tt.execution); got != tt.want {
				t.Fatalf("hasForwardResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBody_ReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "openai flat",
			body: `{"model":"gpt-5","reasoning_effort":"x-high"}`,
			want: "xhigh",
		},
		{
			name: "openai nested",
			body: `{"model":"gpt-5","reasoning":{"effort":"high"}}`,
			want: "high",
		},
		{
			name: "anthropic output effort",
			body: `{"model":"claude-opus-4-6","output_config":{"effort":"max"}}`,
			want: "max",
		},
		{
			name: "anthropic output maximum effort",
			body: `{"model":"claude-opus-4-6","output_config":{"effort":"maximum"}}`,
			want: "max",
		},
		{
			name: "anthropic output ultra effort",
			body: `{"model":"claude-opus-4-6","output_config":{"effort":"ultra"}}`,
			want: "ultra",
		},
		{
			name: "anthropic default",
			body: `{"model":"claude-opus-4-6","thinking":{"type":"enabled","budget_tokens":32768}}`,
			want: "high",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed := parseBody([]byte(tt.body), "application/json")
			if parsed.ReasoningEffort != tt.want {
				t.Fatalf("ReasoningEffort = %q, want %q", parsed.ReasoningEffort, tt.want)
			}
		})
	}
}

func TestBuildPluginRequestUsesWriterForStreamRequest(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	state := &forwardState{
		requestPath: "/v1/images/generations",
		stream:      true,
		realtime:    true,
		keyInfo:     &auth.APIKeyInfo{},
		account:     &ent.Account{},
	}

	req := buildPluginRequest(c, state)
	if !req.Stream {
		t.Fatalf("Stream = false, want true")
	}
	if req.Writer == nil {
		t.Fatalf("Writer = nil, want stream writer")
	}
}

func TestBuildPluginRequestOmitsWriterForPlainNonStreamRequest(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	state := &forwardState{
		requestPath: "/v1/chat/completions",
		stream:      false,
		realtime:    false,
		keyInfo:     &auth.APIKeyInfo{},
		account:     &ent.Account{},
	}

	req := buildPluginRequest(c, state)
	if req.Writer != nil {
		t.Fatalf("Writer = %T, want nil", req.Writer)
	}
}

func TestBuildPluginRequestOmitsWriterForNonStreamImagesRequest(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	state := &forwardState{
		requestPath: "/v1/images/generations",
		stream:      false,
		realtime:    false,
		keyInfo:     &auth.APIKeyInfo{},
		account:     &ent.Account{},
	}

	req := buildPluginRequest(c, state)
	if req.Stream {
		t.Fatalf("Stream = true, want false")
	}
	if req.Writer != nil {
		t.Fatalf("Writer = %T, want nil", req.Writer)
	}
}

func TestBuildPluginRequestSignalsContinuationRecoveryWithoutChangingBody(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":"continue"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("OpenAI-Previous-Response-ID", "resp_header")
	state := &forwardState{
		requestPath:                 "/v1/responses",
		body:                        originalBody,
		continuationRecoveryApplied: true,
		keyInfo:                     &auth.APIKeyInfo{},
		account:                     &ent.Account{},
	}

	req := buildPluginRequest(c, state)
	if string(req.Body) != string(originalBody) {
		t.Fatalf("body = %s, want unchanged %s", req.Body, originalBody)
	}
	if got := req.Headers.Get("OpenAI-Previous-Response-ID"); got != "" {
		t.Fatalf("previous response header = %q, want empty", got)
	}
	if got := req.Headers.Get("X-Airgate-Continuation-Recovery"); got != "drop_previous_response_id" {
		t.Fatalf("recovery header = %q, want drop_previous_response_id", got)
	}
}

func TestCanFailoverImagesUsesOutcomeFailoverPolicy(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	f := &Forwarder{}
	base := &forwardState{requestPath: "/v1/images/generations"}

	if !f.canFailover(c, base, forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited}}) {
		t.Fatal("image 429 should allow failover")
	}
	if !f.canFailover(c, base, forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}}) {
		t.Fatal("image upstream transient should allow failover")
	}
	if f.canFailover(c, base, forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError}}) {
		t.Fatal("image client error should not failover")
	}
	if f.canFailover(c, base, forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError},
		err:     errors.New("context too large"),
	}) {
		t.Fatal("client error with plugin err should not failover")
	}
}

func TestCanFailoverImagesDoesNotRetryAfterStreamWritten(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteHeaderNow()

	f := &Forwarder{}
	state := &forwardState{requestPath: "/v1/images/generations", stream: true}
	if f.canFailover(c, state, forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}}) {
		t.Fatal("image stream should not failover after response is written")
	}
}

func TestCanDispatchCandidateFailoverAdvancesPlan(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	state := &forwardState{
		requestPath: "/v1/images/generations",
		dispatch: newDispatchChain([]sdk.DispatchPlan{
			{SchedulingModel: "gpt-missing"},
			{},
			{SchedulingModel: "gpt-fallback"},
		}),
		dispatchPlan: sdk.DispatchPlan{SchedulingModel: "gpt-missing"},
	}
	state.dispatch.Select(0)
	attempt := maxFailoverAttempts - 1

	ok := (&Forwarder{}).canDispatchCandidateFailover(c, state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind:          sdk.OutcomeClientError,
			FailoverScope: sdk.FailoverScopeDispatchCandidate,
		},
	})
	if !ok {
		t.Fatal("dispatch candidate failover should be allowed")
	}
	if state.dispatch.StartIndex() != 2 {
		t.Fatalf("dispatch start index = %d, want 2", state.dispatch.StartIndex())
	}
	if state.dispatchPlan.SchedulingModel != "gpt-fallback" {
		t.Fatalf("dispatchPlan = %+v, want fallback", state.dispatchPlan)
	}
	if !canStartForwardAttempt(state, attempt) {
		t.Fatal("dispatch candidate failover should not consume the failover attempt budget")
	}
}

func TestCanDispatchCandidateFailoverRequiresScopeAndWritableStream(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	state := &forwardState{
		dispatch: newDispatchChain([]sdk.DispatchPlan{
			{SchedulingModel: "gpt-missing"},
			{SchedulingModel: "gpt-fallback"},
		}),
	}

	if (&Forwarder{}).canDispatchCandidateFailover(c, state, forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError}}) {
		t.Fatal("missing failover scope should not advance candidate")
	}

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteHeaderNow()
	state.stream = true
	if (&Forwarder{}).canDispatchCandidateFailover(c, state, forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError, FailoverScope: sdk.FailoverScopeDispatchCandidate},
	}) {
		t.Fatal("written stream should not advance dispatch candidate")
	}
}

func TestPickAccountRechecksPrimaryAfterFallbackWasSelectedForPoolMiss(t *testing.T) {
	ctx := context.Background()
	restoreRouteGraph := routegraph.SetSnapshotForTesting(nil)
	defer restoreRouteGraph()

	primary := &ent.Account{
		ID:          1,
		Name:        "primary",
		Platform:    "openai",
		State:       account.StateActive,
		ModelPolicy: modelpolicy.Policy{Allow: []string{"gpt-primary"}},
		Extra:       map[string]interface{}{},
	}
	fallback := &ent.Account{
		ID:          2,
		Name:        "fallback",
		Platform:    "openai",
		State:       account.StateActive,
		ModelPolicy: modelpolicy.Policy{Allow: []string{"gpt-fallback"}},
		Extra:       map[string]interface{}{},
	}
	group := &ent.Group{ID: 42, Platform: "openai"}
	group.Edges.Accounts = []*ent.Account{fallback}
	routegraph.SetSnapshotForTesting([]*ent.Group{group})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)

	state := &forwardState{
		requestedPlatform: "openai",
		keyInfo:           &auth.APIKeyInfo{UserID: 7, GroupID: group.ID},
		dispatch: newDispatchChain([]sdk.DispatchPlan{
			{SchedulingModel: "gpt-primary"},
			{SchedulingModel: "gpt-fallback"},
		}),
	}
	forwarder := &Forwarder{scheduler: scheduler.NewScheduler(nil, nil)}

	if err := forwarder.pickAccount(c, state); err != nil {
		t.Fatalf("first pickAccount() error = %v", err)
	}
	if state.account.ID != fallback.ID || state.dispatchPlan.SchedulingModel != "gpt-fallback" {
		t.Fatalf("first pick account=%d plan=%q, want fallback", state.account.ID, state.dispatchPlan.SchedulingModel)
	}
	if got := state.dispatch.StartIndex(); got != 0 {
		t.Fatalf("StartIndex after fallback selected by pool miss = %d, want 0", got)
	}

	group.Edges.Accounts = []*ent.Account{primary, fallback}
	routegraph.SetSnapshotForTesting([]*ent.Group{group})
	state.account = nil
	state.dispatchPlan = sdk.DispatchPlan{}
	if err := forwarder.pickAccount(c, state); err != nil {
		t.Fatalf("second pickAccount() error = %v", err)
	}
	if state.account.ID != primary.ID || state.dispatchPlan.SchedulingModel != "gpt-primary" {
		t.Fatalf("second pick account=%d plan=%q, want primary", state.account.ID, state.dispatchPlan.SchedulingModel)
	}
}

func TestCanStartForwardAttemptStopsImagesAtFailoverCap(t *testing.T) {
	t.Parallel()

	image := &forwardState{requestPath: "/v1/images/generations"}
	if !canStartForwardAttempt(image, maxFailoverAttempts-1) {
		t.Fatal("image submit attempts below the cap should continue")
	}
	if canStartForwardAttempt(image, maxFailoverAttempts) {
		t.Fatal("image submit attempts should stop at the default failover cap")
	}

	chat := &forwardState{requestPath: "/v1/chat/completions"}
	if !canStartForwardAttempt(chat, maxFailoverAttempts-1) {
		t.Fatal("non-image attempts below the cap should continue")
	}
	if canStartForwardAttempt(chat, maxFailoverAttempts) {
		t.Fatal("non-image attempts should stop at the default failover cap")
	}
}

func TestPreferredDifferentAccountTypeOnlyAppliesToThirdAttempt(t *testing.T) {
	t.Parallel()

	previousAccount := &ent.Account{Type: "oauth", Credentials: map[string]string{"plan_type": "team"}}
	const previousType = "oauth:team"
	if got := preferredDifferentAccountTypeForAttempt(0, maxFailoverAttempts, previousAccount); got != "" {
		t.Fatalf("first attempt preference = %q, want empty", got)
	}
	if got := preferredDifferentAccountTypeForAttempt(1, maxFailoverAttempts, previousAccount); got != "" {
		t.Fatalf("second attempt preference = %q, want empty", got)
	}
	if got := preferredDifferentAccountTypeForAttempt(2, maxFailoverAttempts, previousAccount); got != previousType {
		t.Fatalf("third attempt preference = %q, want %q", got, previousType)
	}
}

func TestBuildPluginRequestRemovesPreviousResponseHeadersAfterRecovery(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("x-openai-previous-response-id", "resp_header")
	c.Request.Header.Set("OpenAI-Previous-Response-ID", "resp_header")
	c.Request.Header.Set("previous_response_id", "resp_header")
	state := &forwardState{
		requestPath:                 "/v1/responses",
		keyInfo:                     &auth.APIKeyInfo{},
		account:                     &ent.Account{},
		continuationRecoveryApplied: true,
	}

	req := buildPluginRequest(c, state)
	for _, header := range []string{"x-openai-previous-response-id", "OpenAI-Previous-Response-ID", "previous_response_id"} {
		if got := req.Headers.Get(header); got != "" {
			t.Fatalf("%s = %q, want empty", header, got)
		}
	}
}

func TestBuildHeadersStripsClientControlledAirgateHeaders(t *testing.T) {
	t.Parallel()

	source := http.Header{}
	source.Set("Content-Type", "application/json")
	source.Set("X-Airgate-Operation-Responses-Image-Generation", "true")
	source.Set("X-Airgate-Service-Tier", "priority")
	source.Set("X-Airgate-Force-Instructions", "client-forged")
	source.Set("X-Airgate-Plugin-Openai-Image-Enabled", "true")
	source.Set("X-Airgate-Internal", "client")
	source.Set("X-Airgate-User-ID", "999")
	source.Set("X-Airgate-API-Key-ID", "888")
	source.Set("X-Airgate-Group-ID", "777")
	source.Set("X-Airgate-Task-Execution", "true")
	source.Set("X-Airgate-Task-ID", "123")
	source.Set("X-Airgate-Upstream-Task-ID", "upstream-123")
	source.Set("X-Airgate-Future-Control", "future")
	source["x-airgate-operation-responses-image-generation"] = []string{"true"}
	source["x-airgate-plugin-openai-image-enabled"] = []string{"true"}
	source["x-airgate-service-tier"] = []string{"priority"}

	headers := buildHeaders(source, &auth.APIKeyInfo{
		UserID:  1,
		KeyID:   2,
		GroupID: 3,
	})

	if got := headers.Get("X-Airgate-User-ID"); got != "1" {
		t.Fatalf("X-Airgate-User-ID = %q, want 1", got)
	}
	if got := headers.Get("X-Airgate-API-Key-ID"); got != "2" {
		t.Fatalf("X-Airgate-API-Key-ID = %q, want 2", got)
	}
	if got := headers.Get("X-Airgate-Group-ID"); got != "3" {
		t.Fatalf("X-Airgate-Group-ID = %q, want 3", got)
	}
	for _, header := range []string{
		"X-Airgate-Operation-Responses-Image-Generation",
		"X-Airgate-Service-Tier",
		"X-Airgate-Force-Instructions",
		"X-Airgate-Plugin-Openai-Image-Enabled",
		"X-Airgate-Internal",
		"X-Airgate-Task-ID",
		"X-Airgate-Upstream-Task-ID",
		"X-Airgate-Future-Control",
	} {
		if got := headers.Get(header); got != "" {
			t.Fatalf("%s = %q, want empty", header, got)
		}
	}
	if got := headers.Get("X-Airgate-Task-Execution"); got != "true" {
		t.Fatalf("X-Airgate-Task-Execution = %q, want true", got)
	}
	if got := headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	for _, rawKey := range []string{
		"x-airgate-operation-responses-image-generation",
		"x-airgate-plugin-openai-image-enabled",
		"x-airgate-service-tier",
	} {
		if _, ok := headers[rawKey]; ok {
			t.Fatalf("%s should be stripped", rawKey)
		}
	}
}

func TestBuildHeadersWritesTrustedAirgateControlsAfterStripping(t *testing.T) {
	t.Parallel()

	source := http.Header{}
	source.Set("X-Airgate-Operation-Responses-Image-Generation", "false")
	source.Set("X-Airgate-Service-Tier", "client-tier")
	source.Set("X-Airgate-Force-Instructions", "client-instructions")
	source.Set("X-Airgate-Plugin-Openai-Image-Enabled", "false")

	headers := buildHeaders(source, &auth.APIKeyInfo{
		GroupServiceTier:       "priority",
		GroupForceInstructions: "server-instructions",
		GroupOperationPolicies: map[string]bool{"responses.image_generation": true},
		GroupPluginSettings:    map[string]map[string]string{"openai": {"image_enabled": "true"}},
	})

	if got := headers.Get("X-Airgate-Operation-Responses-Image-Generation"); got != "true" {
		t.Fatalf("operation header = %q, want true", got)
	}
	if got := headers.Get("X-Airgate-Service-Tier"); got != "priority" {
		t.Fatalf("X-Airgate-Service-Tier = %q, want priority", got)
	}
	if got := headers.Get("X-Airgate-Force-Instructions"); got != "server-instructions" {
		t.Fatalf("X-Airgate-Force-Instructions = %q, want server-instructions", got)
	}
	if got := headers.Get("X-Airgate-Plugin-Openai-Image-Enabled"); got != "true" {
		t.Fatalf("plugin setting header = %q, want true", got)
	}
}

func TestRoutesForAPIKeyUsesBoundGroupOnly(t *testing.T) {
	t.Parallel()

	settings := map[string]map[string]string{"openai": {"image_price_1k": "0.01"}}
	state := &forwardState{
		dispatch: newDispatchChain([]sdk.DispatchPlan{{
			Operation: "responses.image_generation",
			Gate: sdk.DispatchGate{
				RequiredOperation: "responses.image_generation",
			},
		}}),
		requirements: routing.Requirements{
			RequiredOperation: "responses.image_generation",
		},
		keyInfo: &auth.APIKeyInfo{
			GroupID:                42,
			GroupPlatform:          "openai",
			GroupRateMultiplier:    1.5,
			UserGroupRates:         map[int64]float64{42: 0.7, 99: 0.1},
			GroupOperationPolicies: map[string]bool{"responses.image_generation": true},
			GroupPluginSettings:    settings,
			GroupServiceTier:       "priority",
			GroupForceInstructions: "stay concise",
		},
	}

	routes := routesForAPIKey(state)
	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	route := routes[0]
	if route.GroupID != 42 {
		t.Fatalf("GroupID = %d, want 42", route.GroupID)
	}
	if route.EffectiveRate != 0.7 {
		t.Fatalf("EffectiveRate = %v, want 0.7", route.EffectiveRate)
	}
	if route.GroupPluginSettings["openai"]["image_price_1k"] != "0.01" {
		t.Fatalf("image price not preserved")
	}

	settings["openai"]["image_price_1k"] = "0.02"
	if route.GroupPluginSettings["openai"]["image_price_1k"] != "0.01" {
		t.Fatalf("route plugin settings should be cloned")
	}
}

func TestRoutesForAPIKeyRejectsImageWhenBoundGroupDisabled(t *testing.T) {
	t.Parallel()

	state := &forwardState{
		dispatch: newDispatchChain([]sdk.DispatchPlan{{
			Operation: "responses.image_generation",
			Gate: sdk.DispatchGate{
				RequiredOperation: "responses.image_generation",
			},
		}}),
		requirements: routing.Requirements{
			RequiredOperation: "responses.image_generation",
		},
		keyInfo: &auth.APIKeyInfo{
			GroupID:                42,
			GroupPlatform:          "openai",
			GroupOperationPolicies: map[string]bool{},
		},
	}

	routes := routesForAPIKey(state)
	if len(routes) != 0 {
		t.Fatalf("len(routes) = %d, want 0", len(routes))
	}
}

func TestRoutesForAPIKeyAllowsChatWithoutOperationGate(t *testing.T) {
	t.Parallel()

	state := &forwardState{keyInfo: &auth.APIKeyInfo{
		GroupID:                42,
		GroupPlatform:          "openai",
		GroupOperationPolicies: map[string]bool{"responses.image_generation": true},
	}}

	routes := routesForAPIKey(state)
	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
}

func TestAPIKeyGroupRequirementErrorImageDisabled(t *testing.T) {
	t.Parallel()

	result := apiKeyGroupMatchResult(&forwardState{
		requirements: routing.Requirements{
			RequiredOperation: "responses.image_generation",
			Status:            http.StatusForbidden,
			ErrorType:         "invalid_request_error",
			Code:              "responses_image_generation_disabled",
			Message:           "当前分组未开启文本路径图片生成功能",
		},
		keyInfo: &auth.APIKeyInfo{
			GroupPlatform:          "openai",
			GroupOperationPolicies: map[string]bool{},
		},
	})
	if result.OK {
		t.Fatal("OK = true, want false")
	}
	if result.Status != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", result.Status, http.StatusForbidden)
	}
	if result.Code != "responses_image_generation_disabled" {
		t.Fatalf("code = %q, want responses_image_generation_disabled", result.Code)
	}
	if result.Message != "当前分组未开启文本路径图片生成功能" {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestAPIKeyGroupRequirementAllowsPlainChat(t *testing.T) {
	t.Parallel()

	result := apiKeyGroupMatchResult(&forwardState{
		keyInfo: &auth.APIKeyInfo{
			GroupPlatform:          "openai",
			GroupOperationPolicies: map[string]bool{"responses.image_generation": true},
		},
	})
	if !result.OK {
		t.Fatalf("OK = false, want true with no operation gate")
	}
}

func TestSelectAllRoutesFailureResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		summary    allRoutesFailureSummary
		wantStatus int
		wantCode   string
	}{
		{
			name: "continuation affinity missing",
			summary: allRoutesFailureSummary{
				continuationAffinityMissing: true,
			},
			wantStatus: http.StatusBadRequest,
			wantCode:   "continuation_affinity_missing",
		},
		{
			name: "upstream rate limited",
			summary: allRoutesFailureSummary{
				rateLimitedSeen:       true,
				rateLimitedRetryAfter: 3 * time.Second,
				upstreamFailureSeen:   true,
			},
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "all_routes_rate_limited",
		},
		{
			name: "continuation unavailable",
			summary: allRoutesFailureSummary{
				continuationUnavailable: true,
				upstreamFailureSeen:     true,
			},
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "continuation_unavailable",
		},
		{
			name: "local capacity exhausted",
			summary: allRoutesFailureSummary{
				localCapacitySeen:   true,
				upstreamFailureSeen: true,
			},
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "all_routes_capacity_exhausted",
		},
		{
			name: "upstream timeout",
			summary: allRoutesFailureSummary{
				upstreamTimeoutSeen: true,
				upstreamFailureSeen: true,
			},
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   "upstream_timeout",
		},
		{
			name: "upstream failure",
			summary: allRoutesFailureSummary{
				upstreamFailureSeen: true,
			},
			wantStatus: http.StatusBadGateway,
			wantCode:   "upstream_error",
		},
		{
			name: "account unavailable",
			summary: allRoutesFailureSummary{
				accountDeadSeen:    true,
				accountUnavailable: true,
			},
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "all_routes_account_unavailable",
		},
		{
			name: "account unavailable by pick error",
			summary: allRoutesFailureSummary{
				accountUnavailable: true,
			},
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "all_routes_account_unavailable",
		},
		{
			name: "account dead only",
			summary: allRoutesFailureSummary{
				accountDeadSeen: true,
			},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "no_available_account",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := selectAllRoutesFailureResponse(tt.summary)
			if got.status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", got.status, tt.wantStatus)
			}
			if got.code != tt.wantCode {
				t.Fatalf("code = %q, want %q", got.code, tt.wantCode)
			}
		})
	}
}

func TestSelectAllRoutesFailureResponse_AccountUnavailableIsRateLimited(t *testing.T) {
	t.Parallel()

	got := selectAllRoutesFailureResponse(allRoutesFailureSummary{accountUnavailable: true})

	if got.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", got.status, http.StatusTooManyRequests)
	}
	if got.errType != "rate_limit_error" {
		t.Fatalf("errType = %q, want rate_limit_error", got.errType)
	}
	if got.message != "当前模型暂无可用上游账号，请稍后重试" {
		t.Fatalf("message = %q, want account unavailable message", got.message)
	}
	if got.retryAfter != allRoutesFailedDefaultRetryAfter {
		t.Fatalf("retryAfter = %s, want %s", got.retryAfter, allRoutesFailedDefaultRetryAfter)
	}
}

func TestRecordAllRoutesAccountUnavailableWritesMonitorEvent(t *testing.T) {
	t.Parallel()

	recorder := &captureMonitorRecorder{}
	forwarder := &Forwarder{monitor: recorder}
	state := &forwardState{
		requestPath:       "/v1/chat/completions",
		model:             "gpt-4.1",
		dispatchPlan:      sdk.DispatchPlan{SchedulingModel: "gpt-4.1-mini"},
		requestedPlatform: "openai",
		plugin:            &PluginInstance{Name: "openai", Platform: "openai"},
		keyInfo: &auth.APIKeyInfo{
			KeyID:     11,
			UserID:    22,
			GroupID:   33,
			GroupName: "production",
		},
	}
	response := allRoutesFailureResponse{
		status:  http.StatusTooManyRequests,
		code:    "all_routes_account_unavailable",
		message: "当前模型暂无可用上游账号，请稍后重试",
	}

	forwarder.recordAllRoutesAccountUnavailable(nil, state, allRoutesFailureSummary{accountUnavailable: true}, response, 4)

	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.Type != monitoring.TypeSchedulerError {
		t.Fatalf("type = %q, want %q", event.Type, monitoring.TypeSchedulerError)
	}
	if event.Source != monitoring.SourceForwarder {
		t.Fatalf("source = %q, want %q", event.Source, monitoring.SourceForwarder)
	}
	if event.SubjectType != monitoring.SubjectScheduler || event.SubjectID != "33" {
		t.Fatalf("subject = %q/%q, want scheduler/33", event.SubjectType, event.SubjectID)
	}
	if event.Platform != "openai" || event.PluginID != "openai" {
		t.Fatalf("locator = %q/%q, want openai/openai", event.Platform, event.PluginID)
	}
	if event.ErrorCode != "all_routes_account_unavailable" {
		t.Fatalf("errorCode = %q, want all_routes_account_unavailable", event.ErrorCode)
	}
	if event.Message != "当前模型暂无可用上游账号，请稍后重试" {
		t.Fatalf("message = %q, want account unavailable message", event.Message)
	}
	if got := event.Detail["attempts"]; got != 4 {
		t.Fatalf("detail attempts = %#v, want 4", got)
	}
	if got := event.Detail["group_name"]; got != "production" {
		t.Fatalf("detail group_name = %#v, want production", got)
	}
}

func TestRecordAPIRequestErrorIncludesGroupSnapshotInDetail(t *testing.T) {
	t.Parallel()

	recorder := &captureRequestMonitorRecorder{}
	forwarder := &Forwarder{requestMonitor: recorder}
	keyInfo := &auth.APIKeyInfo{
		KeyID:     11,
		KeyName:   "default key",
		UserID:    22,
		UserEmail: "user@example.com",
		GroupID:   33,
		GroupName: "production",
	}

	c, _ := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	c.Set(ginCtxKeyAttempts, 4)
	forwarder.recordAPIRequestErrorForKey(c, keyInfo, "openai", "/v1/chat/completions", "gpt-4.1", http.StatusTooManyRequests, "all_routes_account_unavailable", "当前模型暂无可用上游账号，请稍后重试")

	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	event := recorder.events[0]
	if event.GroupID == nil || *event.GroupID != 33 {
		t.Fatalf("groupID = %#v, want 33", event.GroupID)
	}
	if got := event.Detail["group_name"]; got != "production" {
		t.Fatalf("detail group_name = %#v, want production", got)
	}
	if got := event.Detail["api_key_name"]; got != "default key" {
		t.Fatalf("detail api_key_name = %#v, want default key", got)
	}
	if got := event.Detail["attempts"]; got != 4 {
		t.Fatalf("detail attempts = %#v, want 4", got)
	}
	if got := event.Detail["retry_count"]; got != 3 {
		t.Fatalf("detail retry_count = %#v, want 3", got)
	}
}

func TestAllRoutesFailureSummaryRecordsTimeout(t *testing.T) {
	t.Parallel()

	summary := allRoutesFailureSummary{}
	summary.recordExecution(forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient},
		err:     context.DeadlineExceeded,
	})

	if !summary.upstreamTimeoutSeen {
		t.Fatalf("upstreamTimeoutSeen = false, want true")
	}
	if summary.upstreamFailureSeen {
		t.Fatalf("upstreamFailureSeen = true, want false")
	}
}

func TestAllRoutesFailureSummaryRecordsContinuationCapacity(t *testing.T) {
	t.Parallel()

	summary := allRoutesFailureSummary{}
	summary.recordPickAccountError(scheduler.ErrContinuationCapacityExceeded)

	if !summary.continuationUnavailable {
		t.Fatalf("continuationUnavailable = false, want true")
	}
	if summary.localCapacitySeen {
		t.Fatalf("localCapacitySeen = true, want false")
	}
	if summary.continuationAffinityMissing {
		t.Fatalf("continuationAffinityMissing = true, want false")
	}
	response := selectAllRoutesFailureResponse(summary)
	if response.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.status, http.StatusTooManyRequests)
	}
	if response.code != "continuation_unavailable" {
		t.Fatalf("code = %q, want continuation_unavailable", response.code)
	}
}

func TestAllRoutesFailureSummaryRecordsContinuationRecoveryContextTooLarge(t *testing.T) {
	t.Parallel()

	summary := allRoutesFailureSummary{}
	if !summary.recordContinuationRecoveryError(errContinuationRecoveryContextTooLarge) {
		t.Fatalf("recordContinuationRecoveryError = false, want true")
	}

	response := selectAllRoutesFailureResponse(summary)
	if response.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.status, http.StatusRequestEntityTooLarge)
	}
	if response.errType != "invalid_request_error" {
		t.Fatalf("errType = %q, want invalid_request_error", response.errType)
	}
	if response.code != "context_too_large" {
		t.Fatalf("code = %q, want context_too_large", response.code)
	}
	if response.message != contextTooLargeMessage {
		t.Fatalf("message = %q, want contextTooLargeMessage", response.message)
	}
}

func TestRecoverContinuationPickAccountErrorHandlesCapacityWhenSafe(t *testing.T) {
	t.Parallel()

	state := &forwardState{
		body:                        []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`),
		previousResponseID:          "resp_old",
		requireContinuationAffinity: true,
	}

	handled, recovered, err := recoverContinuationPickAccountError(state, scheduler.ErrContinuationCapacityExceeded)
	if err != nil {
		t.Fatalf("recoverContinuationPickAccountError error: %v", err)
	}
	if !handled {
		t.Fatalf("handled = false, want true")
	}
	if !recovered {
		t.Fatalf("recovered = false, want true")
	}
	if state.previousResponseID != "" {
		t.Fatalf("previousResponseID = %q, want empty", state.previousResponseID)
	}
	if state.requireContinuationAffinity {
		t.Fatalf("requireContinuationAffinity = true, want false")
	}
}

func TestRecoverContinuationPickAccountErrorKeepsUnsafeCapacityHard(t *testing.T) {
	t.Parallel()

	state := &forwardState{
		body:                        []byte(`{"model":"gpt-5.4","previous_response_id":"resp_old","input":[{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`),
		previousResponseID:          "resp_old",
		requireContinuationAffinity: true,
	}

	handled, recovered, err := recoverContinuationPickAccountError(state, scheduler.ErrContinuationCapacityExceeded)
	if err != nil {
		t.Fatalf("recoverContinuationPickAccountError error: %v", err)
	}
	if !handled {
		t.Fatalf("handled = false, want true")
	}
	if recovered {
		t.Fatalf("recovered = true, want false")
	}
	if state.previousResponseID != "resp_old" {
		t.Fatalf("previousResponseID = %q, want resp_old", state.previousResponseID)
	}
	if !state.requireContinuationAffinity {
		t.Fatalf("requireContinuationAffinity = false, want true")
	}
}
