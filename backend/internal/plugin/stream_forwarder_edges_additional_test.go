package plugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/routing"
	pb "github.com/DevilGenius/airgate-sdk/protocol/proto"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestHostStreamWritersBufferAndCommit(t *testing.T) {
	stream := &captureHostInvokeStream{ctx: context.Background()}
	writer := &hostStreamWriter{stream: stream}
	writer.Header().Set("X-Test", "yes")

	n, err := writer.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("hostStreamWriter.Write() = %d, %v", n, err)
	}
	if n, err := writer.Write(nil); err != nil || n != 0 {
		t.Fatalf("hostStreamWriter.Write(nil) = %d, %v", n, err)
	}
	writer.WriteHeader(http.StatusCreated)
	writer.Flush()
	if len(stream.sent) != 2 || stream.sent[0].Event != "headers" || stream.sent[1].Event != "chunk" {
		t.Fatalf("host stream frames = %+v", stream.sent)
	}

	stream = &captureHostInvokeStream{ctx: context.Background()}
	target := &hostStreamWriter{stream: stream}
	failover := &failoverStreamWriter{target: target}
	failover.Header().Set("X-Buffered", "yes")
	if n, err := failover.Write([]byte("buffered")); err != nil || n != len("buffered") {
		t.Fatalf("failover Write() = %d, %v", n, err)
	}
	if len(stream.sent) != 0 {
		t.Fatalf("buffered writer sent frames before commit: %+v", stream.sent)
	}
	failover.WriteHeader(http.StatusOK)
	if !failover.committed || len(stream.sent) != 2 {
		t.Fatalf("failover committed=%v frames=%+v", failover.committed, stream.sent)
	}
	if _, err := failover.Write([]byte("after")); err != nil {
		t.Fatalf("committed failover Write() error = %v", err)
	}
	failover.Flush()
	if len(stream.sent) != 3 {
		t.Fatalf("committed frames = %+v", stream.sent)
	}

	stream = &captureHostInvokeStream{ctx: context.Background()}
	errorTarget := &hostStreamWriter{stream: stream}
	errorFailover := &failoverStreamWriter{target: errorTarget}
	errorFailover.Header().Set("X-Error", "yes")
	_, _ = errorFailover.Write([]byte("error body"))
	errorFailover.WriteHeader(http.StatusBadGateway)
	if errorFailover.committed || len(stream.sent) != 0 {
		t.Fatalf("error failover should stay buffered, committed=%v frames=%+v", errorFailover.committed, stream.sent)
	}
	errorFailover.flush()
	if !errorFailover.committed || len(stream.sent) != 2 {
		t.Fatalf("manual flush committed=%v frames=%+v", errorFailover.committed, stream.sent)
	}
	errorFailover.flush()
}

func TestHostInvokeStreamValidation(t *testing.T) {
	handle := (&HostService{}).NewPluginHandle("plugin")
	handle.SetCapabilities(map[sdk.Capability]bool{sdk.CapabilityHostInvoke: true})

	emptyMethod := &captureHostInvokeStream{
		ctx:      context.Background(),
		incoming: []*pb.HostStreamFrame{{}},
	}
	if err := handle.InvokeStream(emptyMethod); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty method InvokeStream error = %v", err)
	}

	unknown := &captureHostInvokeStream{
		ctx:      context.Background(),
		incoming: []*pb.HostStreamFrame{{Method: "unknown.stream"}},
	}
	if err := handle.InvokeStream(unknown); status.Code(err) != codes.Unimplemented {
		t.Fatalf("unknown method InvokeStream error = %v", err)
	}

	denied := (&HostService{}).NewPluginHandle("plugin")
	denied.SetCapabilities(map[sdk.Capability]bool{})
	stream := &captureHostInvokeStream{
		ctx:      context.Background(),
		incoming: []*pb.HostStreamFrame{{Method: hostMethodGatewayForward}},
	}
	if err := denied.InvokeStream(stream); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied InvokeStream error = %v", err)
	}
}

func TestForwarderEdgeHelpers(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	markCanceledRequest(nil, http.StatusGatewayTimeout)
	markCanceledRequest(c, 0)
	markCanceledRequest(c, statusClientClosedRequest)
	if c.Writer.Status() != statusClientClosedRequest {
		t.Fatalf("markCanceledRequest status = %d, want %d", c.Writer.Status(), statusClientClosedRequest)
	}
	c.Writer.WriteHeaderNow()
	markCanceledRequest(c, http.StatusGatewayTimeout)
	if c.Writer.Status() != statusClientClosedRequest {
		t.Fatalf("markCanceledRequest should not rewrite written status, got %d", c.Writer.Status())
	}

	if returnableUpstream(sdk.UpstreamResponse{}) {
		t.Fatal("empty upstream should not be returnable")
	}
	if !returnableUpstream(sdk.UpstreamResponse{StatusCode: http.StatusAccepted, Body: []byte("ok")}) {
		t.Fatal("status with body should be returnable")
	}

	var summary allRoutesFailureSummary
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited, RetryAfter: 5 * time.Second}})
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited, RetryAfter: 2 * time.Second}})
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountDead}})
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountUnavailable}})
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeFamilyTransient}})
	summary.recordExecution(forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUnknown}, err: errors.New("boom")})
	summary.recordPickAccountError(errors.New("plain"))
	summary.recordLocalCapacityFailure()
	if !summary.rateLimitedSeen || summary.rateLimitedRetryAfter != 2*time.Second || !summary.accountDeadSeen ||
		!summary.accountUnavailable || !summary.upstreamFailureSeen || !summary.localCapacitySeen {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.recordContinuationRecoveryError(errors.New("other")) {
		t.Fatal("unrelated continuation recovery error should return false")
	}

	handled, _, err := (&Forwarder{}).recoverContinuationPickAccountError(&forwardState{}, errors.New("plain"))
	if handled || err != nil {
		t.Fatalf("non-recoverable method recover = %v, %v", handled, err)
	}
	if !isOptionalTaskExtensionUnavailable(status.Error(codes.Unimplemented, "missing")) {
		t.Fatal("unimplemented should be optional task extension unavailable")
	}
	if isOptionalTaskExtensionUnavailable(status.Error(codes.Internal, "boom")) {
		t.Fatal("internal error should not be optional task extension unavailable")
	}
}

func TestAllRoutesFailureResponsesAndFailoverDecisions(t *testing.T) {
	responses := []struct {
		name        string
		summary     allRoutesFailureSummary
		status      int
		code        string
		retryHeader bool
	}{
		{name: "context too large", summary: allRoutesFailureSummary{contextTooLargeSeen: true}, status: http.StatusRequestEntityTooLarge, code: "context_too_large"},
		{name: "continuation affinity missing", summary: allRoutesFailureSummary{continuationAffinityMissing: true}, status: http.StatusBadRequest, code: "continuation_affinity_missing"},
		{name: "rate limited", summary: allRoutesFailureSummary{rateLimitedSeen: true, rateLimitedRetryAfter: 1500 * time.Millisecond}, status: http.StatusTooManyRequests, code: "all_routes_rate_limited", retryHeader: true},
		{name: "continuation unavailable", summary: allRoutesFailureSummary{continuationUnavailable: true}, status: http.StatusTooManyRequests, code: "continuation_unavailable", retryHeader: true},
		{name: "local capacity", summary: allRoutesFailureSummary{localCapacitySeen: true}, status: http.StatusTooManyRequests, code: "all_routes_capacity_exhausted", retryHeader: true},
		{name: "upstream timeout", summary: allRoutesFailureSummary{upstreamTimeoutSeen: true}, status: http.StatusGatewayTimeout, code: "upstream_timeout"},
		{name: "upstream failure", summary: allRoutesFailureSummary{upstreamFailureSeen: true}, status: http.StatusBadGateway, code: "upstream_error"},
		{name: "account unavailable", summary: allRoutesFailureSummary{accountUnavailable: true}, status: http.StatusTooManyRequests, code: "all_routes_account_unavailable", retryHeader: true},
		{name: "account dead", summary: allRoutesFailureSummary{accountDeadSeen: true}, status: http.StatusServiceUnavailable, code: "no_available_account"},
		{name: "default", summary: allRoutesFailureSummary{}, status: http.StatusServiceUnavailable, code: "all_routes_failed"},
	}
	for _, tt := range responses {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := pluginTestContext(http.MethodPost, "/v1/chat/completions")
			writeAllRoutesFailed(c, tt.summary)
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.status, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tt.code) {
				t.Fatalf("body missing code %q: %s", tt.code, recorder.Body.String())
			}
			if got := recorder.Header().Get("Retry-After"); tt.retryHeader && got == "" {
				t.Fatalf("Retry-After header missing for %s", tt.name)
			} else if !tt.retryHeader && got != "" {
				t.Fatalf("unexpected Retry-After header %q for %s", got, tt.name)
			}
		})
	}

	for _, tt := range []struct {
		name    string
		written bool
		state   *forwardState
		exec    forwardExecution
		want    bool
	}{
		{name: "stream already written", written: true, state: &forwardState{stream: true}, exec: forwardExecution{err: errors.New("boom")}, want: false},
		{name: "continuation required", state: &forwardState{requireContinuationAffinity: true}, exec: forwardExecution{err: errors.New("boom")}, want: false},
		{name: "client error", state: &forwardState{}, exec: forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError}}, want: false},
		{name: "plugin error", state: &forwardState{}, exec: forwardExecution{err: errors.New("boom")}, want: true},
		{name: "account rate limited", state: &forwardState{}, exec: forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited}}, want: true},
		{name: "family transient", state: &forwardState{}, exec: forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeFamilyTransient}}, want: true},
		{name: "success", state: &forwardState{}, exec: forwardExecution{outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}}, want: false},
	} {
		t.Run("can failover "+tt.name, func(t *testing.T) {
			c, _ := pluginTestContext(http.MethodPost, "/v1/chat/completions")
			if tt.written {
				c.Writer.WriteHeaderNow()
			}
			if got := (&Forwarder{}).canFailover(c, tt.state, tt.exec); got != tt.want {
				t.Fatalf("canFailover() = %v, want %v", got, tt.want)
			}
		})
	}
}

type timeoutFailureErr struct{}

func (timeoutFailureErr) Error() string   { return "temporary timeout" }
func (timeoutFailureErr) Timeout() bool   { return true }
func (timeoutFailureErr) Temporary() bool { return true }

func TestIsTimeoutFailureVariants(t *testing.T) {
	if !isTimeoutFailure(forwardExecution{outcome: sdk.ForwardOutcome{Upstream: sdk.UpstreamResponse{StatusCode: http.StatusGatewayTimeout}}}) {
		t.Fatal("gateway timeout status should be timeout failure")
	}
	if !isTimeoutFailure(forwardExecution{err: context.DeadlineExceeded}) {
		t.Fatal("context deadline should be timeout failure")
	}
	if !isTimeoutFailure(forwardExecution{err: timeoutFailureErr{}}) {
		t.Fatal("Timeout() error should be timeout failure")
	}
	if !isTimeoutFailure(forwardExecution{outcome: sdk.ForwardOutcome{Reason: "upstream timed out"}}) {
		t.Fatal("timeout reason should be timeout failure")
	}
	if isTimeoutFailure(forwardExecution{outcome: sdk.ForwardOutcome{Reason: "connection reset"}}) {
		t.Fatal("non-timeout reason should not be timeout failure")
	}
}

func TestForwarderRouteAndOutcomeTinyHelpers(t *testing.T) {
	baseSettings := map[string]map[string]string{"openai": {"image_enabled": "true"}}
	basePolicies := map[string]bool{"responses.image_generation": true}
	base := &auth.APIKeyInfo{
		KeyID:                  1,
		UserID:                 2,
		GroupID:                3,
		GroupPlatform:          "openai",
		GroupRateMultiplier:    1.2,
		GroupServiceTier:       "priority",
		GroupForceInstructions: "force",
		GroupOperationPolicies: basePolicies,
		GroupPluginSettings:    baseSettings,
	}
	route := routing.Candidate{
		GroupID:                4,
		Platform:               "anthropic",
		GroupRateMultiplier:    2,
		GroupServiceTier:       "standard",
		GroupForceInstructions: "route",
		GroupOperationPolicies: map[string]bool{"chat": true},
		GroupPluginSettings:    map[string]map[string]string{"claude": {"code_only": "true"}},
	}
	got := keyInfoForRoute(base, route)
	if got.GroupID != 4 || got.GroupPlatform != "anthropic" || got.GroupRateMultiplier != 2 ||
		got.GroupServiceTier != "standard" || got.GroupForceInstructions != "route" {
		t.Fatalf("keyInfoForRoute = %+v", got)
	}
	got.GroupPluginSettings["claude"]["code_only"] = "false"
	if route.GroupPluginSettings["claude"]["code_only"] != "true" {
		t.Fatal("keyInfoForRoute should clone plugin settings")
	}
	if apiKeyGroupMatchResult(&forwardState{}).OK {
		t.Fatal("empty API key group match should not be OK")
	}
	if entGroupFromKeyInfo(nil) != nil {
		t.Fatal("nil key info should produce nil ent group")
	}

	if judgmentReason(forwardExecution{outcome: sdk.ForwardOutcome{Reason: "reason"}, err: errors.New("err")}) != "reason" {
		t.Fatal("judgmentReason should prefer outcome reason")
	}
	if judgmentReason(forwardExecution{err: errors.New("err")}) != "err" {
		t.Fatal("judgmentReason should use error")
	}
	if judgmentReason(forwardExecution{}) != "" {
		t.Fatal("empty judgmentReason should be empty")
	}
	if got := resolveReasoningEffort("low", nil); got != "low" {
		t.Fatalf("resolveReasoningEffort request = %q", got)
	}
	if got := resolveReasoningEffort("", nil); got != "" {
		t.Fatalf("resolveReasoningEffort empty = %q", got)
	}

	release, ok := (&HostService{}).acquireHostForwardAccountCapacity(context.Background(), nil)
	if !ok {
		t.Fatal("nil host concurrency should allow capacity")
	}
	release()
	if hostSDKAccount(&ent.Account{ID: 1}).ID != 1 {
		t.Fatal("hostSDKAccount should copy id")
	}
}

type captureHostInvokeStream struct {
	ctx      context.Context
	incoming []*pb.HostStreamFrame
	sent     []*pb.HostStreamFrame
}

func (s *captureHostInvokeStream) Send(frame *pb.HostStreamFrame) error {
	s.sent = append(s.sent, frame)
	return nil
}

func (s *captureHostInvokeStream) Recv() (*pb.HostStreamFrame, error) {
	if len(s.incoming) == 0 {
		return nil, io.EOF
	}
	frame := s.incoming[0]
	s.incoming = s.incoming[1:]
	return frame, nil
}

func (s *captureHostInvokeStream) SetHeader(metadata.MD) error  { return nil }
func (s *captureHostInvokeStream) SendHeader(metadata.MD) error { return nil }
func (s *captureHostInvokeStream) SetTrailer(metadata.MD)       {}
func (s *captureHostInvokeStream) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
func (s *captureHostInvokeStream) SendMsg(any) error { return nil }
func (s *captureHostInvokeStream) RecvMsg(any) error { return nil }
