package monitor

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
	"github.com/DevilGenius/airgate-sdk/sdkgo/requesttrace"

	"github.com/DevilGenius/airgate-core/internal/app/monitor/traceredaction"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

func TestTracePersistenceUsesSharedRedactionPolicy(t *testing.T) {
	raw := []byte(`{"api_key":"body_secret","partial_image_b64":"image_secret","prompt":"keep prompt"}`)
	headers := http.Header{"Content-Type": {"application/json"}, "Authorization": {"header_secret"}, "session_id": {"session-secret"}}
	input := requestmonitoring.TraceInput{Path: "/v1/responses", RequestHeaders: headers, RequestBody: raw, Final: requestmonitoring.TraceFinalError{Message: "password=summary_secret user@example.test"}, Attempts: []requestmonitoring.TraceAttempt{{Reason: "Bearer reason_secret", PluginError: "api_key=plugin_secret", UpstreamHeaders: headers, UpstreamBody: raw, UpstreamErrorBody: raw, OutboundRequests: []requestmonitoring.TraceOutboundRequest{{URL: "https://user:url_secret@example.test/responses?key=query_secret#fragment_secret", Headers: headers, Body: raw}}}}}
	stored, err := encodeRequestTrace(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredRequestTrace(stored)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"body_secret", "image_secret", "header_secret", "session-secret", "summary_secret", "user@example.test", "reason_secret", "plugin_secret", "url_secret", "query_secret", "fragment_secret"} {
		if bytes.Contains(decoded.Payload, []byte(secret)) {
			t.Fatalf("trace leaked %s", secret)
		}
	}
	var payload requestTracePayload
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	expected := traceredaction.SanitizeBody(raw, "application/json", traceredaction.BodyOptions{})
	if payload.Request.Body.Text != string(expected.Body) || payload.Attempts[0].Outbound[0].Body.Text != string(expected.Body) || payload.Attempts[0].RawError.Text != string(expected.Body) {
		t.Fatal("body rules diverged between trace sections")
	}
	if payload.Attempts[0].Outbound[0].URL != "https://example.test/responses" {
		t.Fatal("URL rules diverged")
	}
	if payload.Request.Headers["x-airgate-trace-session-id-xxh3-128"][0] != traceredaction.HashString("session-secret") {
		t.Fatal("fingerprint rule diverged")
	}
}

func TestImageFormIsOmittedFromStoredIngressAndOutbound(t *testing.T) {
	raw := []byte("photo=cHJpdmF0ZS1pbWFnZQ%3D%3D&prompt=x&api_key=fixture_secret")
	headers := http.Header{"Content-Type": {"application/x-www-form-urlencoded; charset=UTF-8"}}
	input := requestmonitoring.TraceInput{
		Path: "/v1/images/edits", RequestHeaders: headers, RequestBody: raw,
		Attempts: []requestmonitoring.TraceAttempt{{OutboundRequests: []requestmonitoring.TraceOutboundRequest{{
			URL: "https://example.test/v1/images/edits", Headers: headers, Body: raw, BodyOriginalSize: int64(len(raw)),
		}}}},
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
	if len(payload.Attempts) != 1 || len(payload.Attempts[0].Outbound) != 1 {
		t.Fatal("missing outbound diagnostics")
	}
	for _, body := range []requestTraceBody{payload.Request.Body, payload.Attempts[0].Outbound[0].Body} {
		if body.Text != "" || body.Base64 != "" || body.Size != 0 || !body.Redacted || body.RedactionReason != "image_input" || body.OriginalSize != int64(len(raw)) {
			t.Fatalf("image form reached persistence: %+v", body)
		}
	}
}

func TestSDKRawCaptureIsRedactedOnlyBeforeStorage(t *testing.T) {
	raw := []byte(`{"access_token":"fixture_access_secret","partial_image_b64":"fixture_image_secret","prompt":"keep prompt"}`)
	headers := http.Header{"Content-Type": {"application/json"}, "Authorization": {"Bearer fixture_header_secret"}, "session_id": {"fixture_session_secret"}}
	ctx, capture := requesttrace.Start(t.Context(), true)
	exchange := requesttrace.Record(ctx, sdk.OutboundRequestDiagnostic{Transport: "websocket", Method: "response.create", URL: "wss://user:fixture_url_secret@example.test/responses?token=fixture_query_secret", Headers: headers, Body: raw})
	exchange.ObserveEvent(raw)
	outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeClientError}
	capture.Finish(&outcome, nil)
	diagnostic := outcome.FinalErrorDiagnostic
	if diagnostic == nil || len(diagnostic.OutboundRequests) != 1 || !bytes.Equal(diagnostic.OutboundRequests[0].Body, raw) || !bytes.Equal(diagnostic.UpstreamErrorBody, raw) || diagnostic.OutboundRequests[0].Headers.Get("Authorization") != "Bearer fixture_header_secret" {
		t.Fatal("SDK changed raw diagnostics")
	}
	outbound := diagnostic.OutboundRequests[0]
	input := requestmonitoring.TraceInput{RequestBody: raw, RequestHeaders: headers, Attempts: []requestmonitoring.TraceAttempt{{UpstreamErrorBody: diagnostic.UpstreamErrorBody, OutboundRequests: []requestmonitoring.TraceOutboundRequest{{Transport: outbound.Transport, Method: outbound.Method, URL: outbound.URL, Headers: outbound.Headers, Body: outbound.Body, BodyOriginalSize: outbound.BodyOriginalSize}}}}}
	stored, err := encodeRequestTrace(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredRequestTrace(stored)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture_access_secret", "fixture_image_secret", "fixture_header_secret", "fixture_session_secret", "fixture_url_secret", "fixture_query_secret"} {
		if bytes.Contains(decoded.Payload, []byte(secret)) {
			t.Fatalf("Core persisted %s", secret)
		}
	}
	if !bytes.Contains(decoded.Payload, []byte("keep prompt")) {
		t.Fatal("Core removed unrelated request data")
	}
	if !bytes.Equal(outbound.Body, raw) {
		t.Fatal("Core redaction modified the SDK snapshot")
	}
}
