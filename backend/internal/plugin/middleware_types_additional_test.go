package plugin

import (
	"errors"
	"net/http"
	"testing"
	"time"

	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/auth"
)

func TestForwardStateSchedulingHelpers(t *testing.T) {
	t.Parallel()

	if (*forwardState)(nil).advanceDispatchCandidate() {
		t.Fatal("nil advanceDispatchCandidate() = true")
	}
	if got := (*forwardState)(nil).schedulingModelCandidates(); got != nil {
		t.Fatalf("nil schedulingModelCandidates() = %#v, want nil", got)
	}
	if got := (*forwardState)(nil).modelForScheduling(); got != "" {
		t.Fatalf("nil modelForScheduling() = %q, want empty", got)
	}

	state := &forwardState{
		model: "fallback-model",
		dispatch: newDispatchChain([]sdk.DispatchPlan{
			{SchedulingModel: " gpt-4.1 "},
			{SchedulingModel: "GPT-4.1"},
			{},
			{SchedulingModel: "gpt-4.1-mini"},
		}),
	}
	got := state.schedulingModelCandidates()
	if len(got) != 2 || got[0] != "gpt-4.1" || got[1] != "gpt-4.1-mini" {
		t.Fatalf("schedulingModelCandidates() = %#v", got)
	}
	if got := (&forwardState{model: "fallback"}).schedulingModelCandidates(); len(got) != 1 || got[0] != "fallback" {
		t.Fatalf("fallback schedulingModelCandidates() = %#v", got)
	}
	if got := (&forwardState{dispatchPlan: sdk.DispatchPlan{SchedulingModel: "selected"}}).modelForScheduling(); got != "selected" {
		t.Fatalf("selected modelForScheduling() = %q", got)
	}
	if got := state.modelForScheduling(); got != " gpt-4.1 " {
		t.Fatalf("chain modelForScheduling() = %q", got)
	}
	state.dispatch.Select(0)
	state.account = &ent.Account{ID: 99}
	if !state.advanceDispatchCandidate() || state.dispatchPlan.SchedulingModel != "GPT-4.1" || state.account != nil {
		t.Fatalf("advanceDispatchCandidate() plan = %+v", state.dispatchPlan)
	}
}

func TestMiddlewareHelpersCloneAndSort(t *testing.T) {
	t.Parallel()

	mgr := &Manager{instances: map[string]*PluginInstance{
		"b": {Name: "b", Priority: 10, Middleware: &sdkgrpc.MiddlewareGRPCClient{}},
		"a": {Name: "a", Priority: 10, Middleware: &sdkgrpc.MiddlewareGRPCClient{}},
		"c": {Name: "c", Priority: 1, Middleware: &sdkgrpc.MiddlewareGRPCClient{}},
		"x": {Name: "x", Priority: 0},
		"n": nil,
	}}
	plugins := mgr.listMiddlewarePlugins()
	if len(plugins) != 3 || plugins[0].Name != "c" || plugins[1].Name != "a" || plugins[2].Name != "b" {
		t.Fatalf("listMiddlewarePlugins() order = %+v", plugins)
	}

	state := &forwardState{
		requestID: "req-1",
		keyInfo:   &auth.APIKeyInfo{UserID: 11, GroupID: 22},
		account:   &ent.Account{ID: 33, Platform: "openai"},
		model:     "gpt-4.1",
		stream:    true,
	}
	bag := map[string]string{"trace": "original"}
	req := buildMiddlewareRequest(state, bag)
	if req.RequestID != "req-1" || req.UserID != 11 || req.GroupID != 22 || req.AccountID != 33 || req.Platform != "openai" || !req.Stream {
		t.Fatalf("middleware request = %+v", req)
	}
	req.Metadata["trace"] = "mutated"
	if bag["trace"] != "original" {
		t.Fatalf("request metadata should be cloned, bag = %#v", bag)
	}

	execution := forwardExecution{
		duration: 150 * time.Millisecond,
		outcome: sdk.ForwardOutcome{
			Kind:   sdk.OutcomeUpstreamTransient,
			Reason: "upstream failed",
			Upstream: sdk.UpstreamResponse{
				StatusCode: http.StatusBadGateway,
			},
		},
		err: errors.New("transport failed"),
	}
	evt := buildMiddlewareEvent(state, execution, bag)
	if evt.StatusCode != http.StatusBadGateway || evt.ErrorKind != sdk.OutcomeUpstreamTransient.String() || evt.ErrorMsg != "upstream failed" {
		t.Fatalf("middleware event = %+v", evt)
	}
	evt.Metadata["trace"] = "mutated"
	if bag["trace"] != "original" {
		t.Fatalf("event metadata should be cloned, bag = %#v", bag)
	}

	mergeMetadata(bag, map[string]string{"next": "value"})
	if bag["next"] != "value" {
		t.Fatalf("mergeMetadata() = %#v", bag)
	}
	if got := cloneMetadata(nil); got != nil {
		t.Fatalf("cloneMetadata(nil) = %#v, want nil", got)
	}
}

func TestMonitorSmallHelpers(t *testing.T) {
	t.Parallel()

	if got := requestMethod(nil, "POST"); got != "POST" {
		t.Fatalf("requestMethod(nil) = %q, want POST", got)
	}
	if got := timeSinceMilliseconds(time.Time{}); got != 0 {
		t.Fatalf("timeSinceMilliseconds(zero) = %d, want 0", got)
	}
	if got := httpErrorClassForStatus(http.StatusTooManyRequests); got != "rate_limit_error" {
		t.Fatalf("429 class = %q", got)
	}
	if got := httpErrorClassForStatus(http.StatusInternalServerError); got != "server_error" {
		t.Fatalf("500 class = %q", got)
	}
	if got := httpErrorClassForStatus(http.StatusBadRequest); got != "invalid_request_error" {
		t.Fatalf("400 class = %q", got)
	}
	if got := requestSeverityForStatus(http.StatusTooManyRequests); got != "warning" {
		t.Fatalf("429 severity = %q", got)
	}
	if got := requestSeverityForStatus(http.StatusBadRequest); got != "info" {
		t.Fatalf("400 severity = %q", got)
	}
	if got := intPtr(0); got != nil {
		t.Fatalf("intPtr(0) = %#v, want nil", got)
	}
	if got := intPtr(7); got == nil || *got != 7 {
		t.Fatalf("intPtr(7) = %#v", got)
	}
}
