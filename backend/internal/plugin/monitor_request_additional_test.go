package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type captureCombinedMonitor struct {
	monitorEvents  []monitoring.EventInput
	requestEvents  []requestmonitoring.EventInput
	recoveryEvents []monitoring.RecoverySuccess
}

func (r *captureCombinedMonitor) Record(_ context.Context, input monitoring.EventInput) {
	r.monitorEvents = append(r.monitorEvents, input)
}

func (r *captureCombinedMonitor) ResolveBySubject(context.Context, monitoring.ResolveQuery) {}

func (r *captureCombinedMonitor) RecordRequest(_ context.Context, input requestmonitoring.EventInput) {
	r.requestEvents = append(r.requestEvents, input)
}

func (r *captureCombinedMonitor) RecordRecoverySuccess(_ context.Context, input monitoring.RecoverySuccess) {
	r.recoveryEvents = append(r.recoveryEvents, input)
}

func pluginTestContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(sdk.WithRequestID(req.Context(), "ctx-request"))
	c.Request = req
	c.Set(middleware.CtxKeyRequestID, "gin-request")
	return c, recorder
}

func testKeyInfo() *auth.APIKeyInfo {
	return &auth.APIKeyInfo{
		KeyID:     11,
		KeyName:   "main",
		UserID:    22,
		UserEmail: "user@example.com",
		GroupID:   33,
		GroupName: "prod",
	}
}

func testForwardState() *forwardState {
	return &forwardState{
		startedAt:         time.Now().Add(-25 * time.Millisecond),
		requestPath:       "/v1/chat/completions",
		model:             "gpt-4.1",
		dispatchPlan:      sdk.DispatchPlan{SchedulingModel: "gpt-4.1-mini"},
		requestedPlatform: "openai",
		plugin:            &PluginInstance{Name: "openai-gateway", Platform: "openai"},
		keyInfo:           testKeyInfo(),
		account:           &ent.Account{ID: 44, Name: "account-a", Platform: "openai", Type: "oauth"},
	}
}

func TestSetMonitorRecorderAlsoSetsRequestRecorder(t *testing.T) {
	recorder := &captureCombinedMonitor{}
	forwarder := &Forwarder{}
	forwarder.SetMonitorRecorder(recorder)
	if forwarder.monitor != recorder || forwarder.requestMonitor != recorder {
		t.Fatalf("recorders not wired: monitor=%T request=%T", forwarder.monitor, forwarder.requestMonitor)
	}
	var nilForwarder *Forwarder
	nilForwarder.SetMonitorRecorder(recorder)
	nilForwarder.SetRequestMonitorRecorder(recorder)
}

func TestPluginMonitorRecordsExecutionClientAndClosedEvents(t *testing.T) {
	c, _ := pluginTestContext(http.MethodPatch, "/v1/chat/completions")
	recorder := &captureCombinedMonitor{}
	forwarder := &Forwarder{requestMonitor: recorder, monitor: recorder}
	state := testForwardState()

	forwarder.recordPluginRouteError(c, state.keyInfo, "openai", state.requestPath, "route_not_found", "unsupported route")
	forwarder.recordPluginExecutionError(sdk.WithRequestID(context.Background(), "exec-request"), state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind:     sdk.OutcomeUpstreamTransient,
			Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway},
			Reason:   "temporary failure",
		},
		duration: 15 * time.Millisecond,
	})
	forwarder.recordPluginExecutionError(context.Background(), state, forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess},
	})
	forwarder.recordClientRequestError(c, state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind: sdk.OutcomeClientError,
			Upstream: sdk.UpstreamResponse{
				StatusCode: http.StatusRequestEntityTooLarge,
				Body:       []byte(`{"error":{"message":"Request entity too large","code":"bad_request"}}`),
			},
			Reason: "bad request",
		},
		duration: time.Second,
	})
	forwarder.recordClientClosedRequest(c, state, statusClientClosedRequest, 3)
	forwarder.recordClientClosedRequest(c, state, 0, 3)

	if len(recorder.requestEvents) != 4 {
		t.Fatalf("request events = %d, want 4: %+v", len(recorder.requestEvents), recorder.requestEvents)
	}
	route := recorder.requestEvents[0]
	if route.Type != requestmonitoring.TypePluginRouteError || route.Method != http.MethodPatch ||
		route.HTTPStatus == nil || *route.HTTPStatus != http.StatusServiceUnavailable ||
		route.APIKeyID == nil || *route.APIKeyID != 11 {
		t.Fatalf("route event = %+v", route)
	}
	exec := recorder.requestEvents[1]
	if exec.Type != requestmonitoring.TypePluginForwardError || exec.RequestID != "exec-request" ||
		exec.PluginID != "openai-gateway" || exec.AccountID == nil || *exec.AccountID != 44 ||
		exec.UpstreamStatus == nil || *exec.UpstreamStatus != http.StatusBadGateway ||
		exec.Detail["stage"] != "plugin_forward" {
		t.Fatalf("execution event = %+v", exec)
	}
	client := recorder.requestEvents[2]
	if client.Type != requestmonitoring.TypeClientRequestError || client.HTTPStatus == nil ||
		*client.HTTPStatus != http.StatusRequestEntityTooLarge || client.Message != imageTooLargeMessage {
		t.Fatalf("client event = %+v", client)
	}
	closed := recorder.requestEvents[3]
	if closed.Type != requestmonitoring.TypeClientClosed || closed.HTTPStatus == nil ||
		*closed.HTTPStatus != statusClientClosedRequest || closed.Detail["attempts"] != 3 {
		t.Fatalf("closed event = %+v", closed)
	}

	forwarder.recordMonitorRecoverySuccess(context.Background(), state)
	if len(recorder.recoveryEvents) != 1 || recorder.recoveryEvents[0].PluginID != "openai-gateway" ||
		recorder.recoveryEvents[0].GroupID != 33 || recorder.recoveryEvents[0].Model != "gpt-4.1-mini" {
		t.Fatalf("recovery events = %+v", recorder.recoveryEvents)
	}
}

func TestPluginMonitorNilAndPureHelperBranches(t *testing.T) {
	recorder := &captureRequestMonitorRecorder{}
	forwarder := &Forwarder{requestMonitor: recorder}
	c, _ := pluginTestContext(http.MethodPost, "/v1/responses")

	forwarder.recordAPIRequestError(c, nil, http.StatusBadRequest, "bad", "bad")
	forwarder.recordAPIRequestErrorForKey(c, nil, "openai", "/v1/responses", "gpt", http.StatusBadRequest, "bad", "bad")
	forwarder.recordPluginRouteError(c, nil, "openai", "/v1/responses", "route", "missing")
	if len(recorder.events) != 1 {
		t.Fatalf("request events after nil guards = %d, want 1", len(recorder.events))
	}
	if recorder.events[0].APIKeyID != nil {
		t.Fatalf("route event should not have key info: %+v", recorder.events[0])
	}

	var input requestmonitoring.EventInput
	attachRequestKeyInfo(&input, testKeyInfo())
	if input.Detail["group_name"] != "prod" || input.APIKeyID == nil || *input.APIKeyID != 11 {
		t.Fatalf("attached key info = %+v", input)
	}
	attachRequestKeyInfo(nil, testKeyInfo())
	attachKeyInfoDetail(nil, testKeyInfo())

	if requestMethod(nil, "POST") != "POST" {
		t.Fatal("nil gin context should use fallback method")
	}
	if timeSinceMilliseconds(time.Time{}) != 0 {
		t.Fatal("zero startedAt should return zero duration")
	}
	if intPtr(0) != nil {
		t.Fatal("intPtr(0) should be nil")
	}
	if !shouldRecordPluginExecutionError(forwardExecution{err: errors.New("boom")}) {
		t.Fatal("plugin execution error with err should be recorded")
	}
	if shouldRecordPluginExecutionError(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError}}) {
		t.Fatal("client errors should not be plugin execution errors")
	}
}

func TestRequestContinuationHeaderAndPlatformBranches(t *testing.T) {
	c, _ := pluginTestContext(http.MethodPost, "/v1/responses")
	c.Request.Header.Set("X-Airgate-Platform", " claude ")
	if got := requestedPlatform(c, &auth.APIKeyInfo{GroupPlatform: "openai"}); got != "claude" {
		t.Fatalf("requestedPlatform header = %q, want claude", got)
	}
	c.Request.Header.Del("X-Airgate-Platform")
	if got := requestedPlatform(c, &auth.APIKeyInfo{GroupPlatform: "openai"}); got != "openai" {
		t.Fatalf("requestedPlatform group = %q, want openai", got)
	}

	headers := http.Header{}
	headers.Set("previous_response_id", " resp_123 ")
	if got := previousResponseIDFromHeaders(headers); got != "resp_123" {
		t.Fatalf("previousResponseIDFromHeaders = %q", got)
	}
	if got := previousResponseIDFromHeaders(nil); got != "" {
		t.Fatalf("nil previousResponseIDFromHeaders = %q", got)
	}

	content := []any{
		map[string]any{"type": "reasoning", "encrypted_content": "cipher"},
		map[string]any{"type": "compaction_summary"},
		map[string]any{"type": "tool_result"},
		map[string]any{"type": "tool_use"},
		"ignored",
	}
	signals := continuationSignals{}
	analyzeMessageContentSignals(content, &signals)
	if !signals.hasEncryptedContent || !signals.hasCompactionReplay || !signals.hasToolOutput || !signals.hasToolCallContext {
		t.Fatalf("content signals = %+v", signals)
	}
	analyzeMessageContentSignals(content, nil)

	messages, err := json.Marshal([]map[string]any{
		{"role": "tool"},
		{"role": "assistant", "function_call": map[string]any{"name": "f"}},
		{"type": "reasoning", "encrypted_content": "cipher", "content": content},
	})
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	signals = analyzeMessagesSignals(messages)
	if !signals.hasEncryptedContent || !signals.hasToolOutput || !signals.hasToolCallContext || !signals.hasCompactionReplay {
		t.Fatalf("message signals = %+v", signals)
	}
	if got := analyzeMessagesSignals([]byte(`{"bad": true}`)); got != (continuationSignals{}) {
		t.Fatalf("invalid message shape signals = %+v", got)
	}

	if !isImageSubmitAPIPath("/V1/Images/Generations?x=1") {
		t.Fatal("normalized image generation path should match")
	}
	if isImageSubmitAPIPath("/v1/chat/completions") {
		t.Fatal("chat path should not be image submit path")
	}
	if asciiHasPrefixFold("x", "x-airgate-") {
		t.Fatal("short string should not have folded prefix")
	}
}

func TestOutcomeResponseIDsAndCredentialSlotBranches(t *testing.T) {
	outcome := sdk.ForwardOutcome{
		Usage: &sdk.Usage{Metadata: map[string]string{
			responseIDUsageMetadataKey: " resp_usage ",
			"openai.response_ids":      "resp_a,ignored,resp_a, resp_b ",
		}},
		Upstream: sdk.UpstreamResponse{Body: []byte(`{"id":"resp_body","response":{"id":"resp_nested"}}`)},
	}
	ids := responseIDsFromOutcome(outcome)
	want := []string{"resp_usage", "resp_a", "resp_b", "resp_body", "resp_nested"}
	if len(ids) != len(want) {
		t.Fatalf("response IDs = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("response IDs = %v, want %v", ids, want)
		}
	}
	if got := responseIDsFromBody([]byte(`bad`)); len(got) != 0 {
		t.Fatalf("bad body IDs = %v", got)
	}

	forwarder := &Forwarder{}
	forwarder.persistUpdatedCredentials(1, nil)
	release := forwarder.acquireCredentialPersistSlot()
	release()

	forwarder.credentialPersistSem = make(chan struct{}, 1)
	release = forwarder.acquireCredentialPersistSlot()
	if len(forwarder.credentialPersistSem) != 1 {
		t.Fatalf("credential semaphore len = %d, want 1", len(forwarder.credentialPersistSem))
	}
	release()
	if len(forwarder.credentialPersistSem) != 0 {
		t.Fatalf("credential semaphore len after release = %d, want 0", len(forwarder.credentialPersistSem))
	}
	firstLock := forwarder.credentialLock(7)
	secondLock := forwarder.credentialLock(7)
	if firstLock != secondLock {
		t.Fatal("credentialLock should reuse per-account mutex")
	}
}

func TestAvailablePlatformsDeduplicatesRunningGatewayPlatforms(t *testing.T) {
	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["openai-a"] = &PluginInstance{Name: "openai-a", Platform: "openai"}
	manager.instances["openai-b"] = &PluginInstance{Name: "openai-b", Platform: "openai"}
	manager.instances["empty"] = &PluginInstance{Name: "empty"}
	manager.instances["claude"] = &PluginInstance{Name: "claude", Platform: "claude"}

	got := availablePlatforms(manager)
	if len(got) != 2 {
		t.Fatalf("availablePlatforms = %v, want two unique platforms", got)
	}
	seen := map[string]bool{}
	for _, platform := range got {
		seen[platform] = true
	}
	if !seen["openai"] || !seen["claude"] {
		t.Fatalf("availablePlatforms = %v", got)
	}
}

func TestCheckBalanceAndClientQuotaNoLimitBranches(t *testing.T) {
	c, recorder := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	forwarder := &Forwarder{}
	if !forwarder.checkBalance(c, &forwardState{requestPath: "/v1/models", keyInfo: &auth.APIKeyInfo{UserBalance: 0}}) {
		t.Fatal("metadata path should bypass balance")
	}
	if !forwarder.checkBalance(c, &forwardState{requestPath: "/v1/chat/completions", keyInfo: &auth.APIKeyInfo{UserBalance: 1}}) {
		t.Fatal("positive balance should pass")
	}
	if forwarder.checkBalance(c, &forwardState{requestPath: "/v1/chat/completions", keyInfo: &auth.APIKeyInfo{UserBalance: 0}}) {
		t.Fatal("zero balance should fail")
	}
	if recorder.Code != http.StatusPaymentRequired {
		t.Fatalf("balance failure status = %d, want %d", recorder.Code, http.StatusPaymentRequired)
	}

	c, _ = pluginTestContext(http.MethodPost, "/v1/chat/completions")
	release := forwarder.acquireClientQuota(c, &forwardState{keyInfo: &auth.APIKeyInfo{UserID: 1, KeyID: 2}})
	if release == nil {
		t.Fatal("no-limit client quota should return release callback")
	}
	release()
}

func TestMatchPluginBranches(t *testing.T) {
	c, _ := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	keyInfo := testKeyInfo()
	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["openai"] = &PluginInstance{Name: "openai", Platform: "openai"}
	manager.instances["claude"] = &PluginInstance{Name: "claude", Platform: "claude"}
	manager.routeCache["openai"] = []sdk.RouteDefinition{{Path: "/v1/chat/completions"}}
	manager.routeCache["claude"] = []sdk.RouteDefinition{{Path: "/v1/messages"}}
	recorder := &captureRequestMonitorRecorder{}
	forwarder := &Forwarder{manager: manager, requestMonitor: recorder}

	if inst := forwarder.matchPlugin(c, keyInfo, "openai", "/v1/chat/completions"); inst == nil || inst.Name != "openai" {
		t.Fatalf("platform route match = %+v", inst)
	}
	if inst := forwarder.matchPlugin(c, keyInfo, "", "/v1/messages/stream"); inst == nil || inst.Name != "claude" {
		t.Fatalf("path prefix route match = %+v", inst)
	}

	c, routeRecorder := pluginTestContext(http.MethodPost, "/v1/unknown")
	if inst := forwarder.matchPlugin(c, keyInfo, "openai", "/v1/unknown"); inst != nil {
		t.Fatalf("route-missing match = %+v, want nil", inst)
	}
	if routeRecorder.Code != http.StatusNotFound {
		t.Fatalf("route-missing status = %d, want %d", routeRecorder.Code, http.StatusNotFound)
	}
	if len(recorder.events) != 1 || recorder.events[0].ErrorCode != "route_not_found" {
		t.Fatalf("route-missing events = %+v", recorder.events)
	}

	c, unavailableRecorder := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	if inst := forwarder.matchPlugin(c, keyInfo, "gemini", "/v1/chat/completions"); inst != nil {
		t.Fatalf("unavailable match = %+v, want nil", inst)
	}
	if unavailableRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status = %d, want %d", unavailableRecorder.Code, http.StatusServiceUnavailable)
	}
	if len(recorder.events) != 2 || recorder.events[1].Type != requestmonitoring.TypePluginRouteError {
		t.Fatalf("unavailable events = %+v", recorder.events)
	}

	c, missingRecorder := pluginTestContext(http.MethodPost, "/v1/nope")
	if inst := forwarder.matchPlugin(c, keyInfo, "", "/v1/nope"); inst != nil {
		t.Fatalf("no-platform missing match = %+v, want nil", inst)
	}
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("no-platform missing status = %d, want %d", missingRecorder.Code, http.StatusNotFound)
	}
}

func TestParseRequestSuccessAndUnauthenticatedBranches(t *testing.T) {
	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["openai"] = &PluginInstance{Name: "openai", Platform: "openai"}
	manager.routeCache["openai"] = []sdk.RouteDefinition{{Path: "/v1/chat/completions"}}
	forwarder := &Forwarder{manager: manager}

	c, _ := pluginTestContext(http.MethodPost, "/v1/chat/completions?trace=1")
	c.Request.Body = http.NoBody
	c.Set(middleware.CtxKeyKeyInfo, "not-key-info")
	if state, ok := forwarder.parseRequest(c); ok || state != nil {
		t.Fatalf("parseRequest with bad key info state=%+v ok=%v", state, ok)
	}

	c, _ = pluginTestContext(http.MethodPost, "/v1/chat/completions?trace=1")
	c.Request.Body = ioNopCloser{strings.NewReader(`{"model":"gpt-4.1","stream":true,"metadata":{"user_id":"user_session_abc"},"previous_response_id":"resp_prev"}`)}
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Airgate-Platform", "openai")
	c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{UserID: 1, KeyID: 2, GroupID: 3, GroupPlatform: "claude"})

	state, ok := forwarder.parseRequest(c)
	if !ok || state == nil {
		t.Fatalf("parseRequest success state=%+v ok=%v", state, ok)
	}
	if state.model != "gpt-4.1" || !state.stream || state.sessionID != "claude:abc" ||
		state.previousResponseID != "resp_prev" || state.requestedPlatform != "openai" ||
		state.plugin == nil || state.plugin.Name != "openai" {
		t.Fatalf("parseRequest state = %+v", state)
	}
}

type ioNopCloser struct {
	*strings.Reader
}

func (c ioNopCloser) Close() error { return nil }

func TestTaskAssetObjectKeyCollectionBranches(t *testing.T) {
	if got := collectTaskAssetObjectKeys(nil); got != nil {
		t.Fatalf("nil task keys = %v, want nil", got)
	}
	task := &ent.Task{
		Attributes: map[string]interface{}{
			taskInputAssetObjectKeysField: []any{"input/a.png", []string{"input/b.png"}, map[string]any{"nested": "input/c.png"}},
		},
		Output: map[string]interface{}{
			taskOutputAssetObjectKeysField: map[string]any{"one": "output/a.png", "empty": ""},
			"nested": []any{
				"see /assets-runtime/tasks%2Fone.png?download=1",
				map[string]any{"html": `<img src="/assets-runtime/tasks/two.png">`},
			},
		},
	}
	keys := collectTaskAssetObjectKeys(task)
	want := []string{"input/a.png", "input/b.png", "input/c.png", "output/a.png", "tasks/one.png", "tasks/two.png"}
	if len(keys) != len(want) {
		t.Fatalf("asset keys = %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("asset keys = %v, want %v", keys, want)
		}
	}
	if key, err := runtimeAssetURLToObjectKey("/assets-runtime/folder/%ZZ"); err == nil || key != "" {
		t.Fatalf("bad runtime asset URL key=%q err=%v, want decode error", key, err)
	}
	if key, err := runtimeAssetURLToObjectKey("/assets-runtime/"); err != nil || key != "" {
		t.Fatalf("empty runtime asset URL key=%q err=%v, want empty nil", key, err)
	}
}
