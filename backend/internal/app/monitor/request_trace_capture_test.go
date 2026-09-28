package monitor

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

func TestIncompleteIngressIsMarkedBeforeTraceStorage(t *testing.T) {
	for _, input := range []requestmonitoring.TraceInput{
		{RequestBodyIncomplete: true},
		{RequestBody: []byte(`{"valid":"but incomplete"}`), RequestBodyIncomplete: true},
		{RequestBodyOriginalSize: 100, RequestBodyIncomplete: true},
		{RequestBody: []byte("short"), RequestBodyOriginalSize: 100},
	} {
		input.RequestHeaders = http.Header{"Content-Type": {"application/json"}}
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
		body := payload.Request.Body
		if body.Text != "" || body.Base64 != "" || body.Hash != "" || body.Size != 0 || !body.Redacted || body.RedactionReason != "trace_capture_incomplete" || body.OriginalSize != max(input.RequestBodyOriginalSize, int64(len(input.RequestBody))) {
			t.Fatalf("incomplete ingress persisted as a complete body: %+v", body)
		}
	}
}

func TestCaptureLengthIsSeparateFromRedaction(t *testing.T) {
	got := buildRequestTraceBody(nil, "application/json", requestTraceBodyOptions{OriginalSize: 99})
	if !got.Redacted || got.RedactionReason != "trace_capture_incomplete" || got.OriginalSize != 99 || got.Size != 0 {
		t.Fatalf("capture omission lost: %+v", got)
	}
	body := []byte(`{"access_token":"fixture_secret","keep":"metadata","BodyRedacted":true,"BodyRedactionReason":"credentials"}`)
	got = buildRequestTraceBody(body, "application/json", requestTraceBodyOptions{OriginalSize: int64(len(body))})
	if !got.Redacted || got.RedactionReason != "credentials" || strings.Contains(got.Text, "fixture_secret") || !strings.Contains(got.Text, "metadata") {
		t.Fatal("caller metadata bypassed Core redaction")
	}
}

func TestCoreOmitsImageBytesWithoutResponseContentType(t *testing.T) {
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0}
	got := buildRequestTraceBody(png, "application/json", requestTraceBodyOptions{})
	if !got.Redacted || got.RedactionReason != "image_input" || got.Size != 0 || got.OriginalSize != int64(len(png)) {
		t.Fatal("raw image response was persisted")
	}
}
