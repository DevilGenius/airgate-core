package monitor

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

func TestRequestTraceRoundTripPreservesIngressAndWireBodies(t *testing.T) {
	original := []byte("{\n \"model\":\"gpt-test\",\"service_tier\":\"priority\",\"input\":\"" + strings.Repeat("full original context ", 4096) + "\",\"extension\":{\"preserve\":true}}")
	wire := []byte(`{"model":"gpt-test","model_selection":"explicit","stream":true,"input":[{"role":"user","content":"converted BPS history"}]}`)
	input := requestmonitoring.TraceInput{
		ObservedAt: time.Now(), Method: "POST", Path: "/v1/responses", Platform: "openai", PluginID: "gateway-openai", Model: "gpt-test",
		RequestBody: original, RequestHeaders: http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer secret"}},
		Attempts: []requestmonitoring.TraceAttempt{{Number: 1, OutcomeKind: "client_error", UpstreamStatus: 422, OutboundRequests: []requestmonitoring.TraceOutboundRequest{{Transport: "http", Method: "POST", URL: "https://bps.openai.com/basispoints/api/responses", Headers: http.Header{"Content-Type": {"application/json"}}, Body: wire, StatusCode: 422}}}},
		Final:    requestmonitoring.TraceFinalError{Stage: "plugin_forward", HTTPStatus: 422, ErrorCode: "invalid_request"},
	}
	stored, err := encodeRequestTrace(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredRequestTrace(stored)
	if err != nil {
		t.Fatal(err)
	}
	var payload requestTracePayload
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Request.Body.Text != string(original) || payload.Request.Body.Redacted {
		t.Fatal("original request was truncated or rewritten")
	}
	if len(payload.Attempts) != 1 || len(payload.Attempts[0].Outbound) != 1 || payload.Attempts[0].Outbound[0].Body.Text != string(wire) {
		t.Fatal("BAS wire request was not preserved separately")
	}
	if len(payload.Request.Headers["Authorization"]) != 0 {
		t.Fatal("stored ingress credentials")
	}
}
