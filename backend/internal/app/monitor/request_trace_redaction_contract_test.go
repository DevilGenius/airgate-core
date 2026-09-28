package monitor

import (
	"strings"
	"testing"

	"github.com/DevilGenius/airgate-sdk/runtimego/requesttrace"
)

func TestAlreadyRedactedTraceBodyPreservesBytesAndMetadata(t *testing.T) {
	// Re-sanitizing this safe payload would re-encode it and discard whitespace.
	body := []byte("{\n  \"access_token\": \"[REDACTED]\", \"text\": \"keep\"\n}")
	contentType := "application/json; charset=utf-8"
	got := buildRequestTraceBody(body, contentType, requestTraceBodyOptions{
		RedactImageInputs: true, AlreadyRedacted: true, RedactionReason: " credentials ", OriginalSize: 4096,
	})
	if got.Text != string(body) || got.ContentType != contentType || got.Size != len(body) || got.Hash != requesttrace.Hash(body) {
		t.Fatalf("already-redacted body was processed again: %+v", got)
	}
	if !got.Redacted || got.RedactionReason != "credentials" || got.OriginalSize != 4096 {
		t.Fatalf("redaction metadata changed: %+v", got)
	}
}

func TestIncompleteRedactionMetadataStillSanitizes(t *testing.T) {
	body := []byte(`{"partial_image_b64":"private-image-bytes","keep":"metadata"}`)
	for _, tc := range []struct {
		name   string
		marked bool
		reason string
	}{
		{"unmarked", false, ""}, {"reason-only", false, "image_input"}, {"flag-only", true, ""}, {"blank-reason", true, "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildRequestTraceBody(body, "application/json", requestTraceBodyOptions{RedactImageInputs: true, AlreadyRedacted: tc.marked, RedactionReason: tc.reason})
			if !got.Redacted || got.RedactionReason != "image_input" || strings.Contains(got.Text, "private-image-bytes") || strings.Contains(got.Text, "partial_image_b64") || !strings.Contains(got.Text, "metadata") {
				t.Fatalf("incomplete marker bypassed redaction: %+v", got)
			}
		})
	}
}

func TestAlreadyOmittedTraceBodyRetainsSize(t *testing.T) {
	got := buildRequestTraceBody(nil, "application/json", requestTraceBodyOptions{RedactImageInputs: true, AlreadyRedacted: true, RedactionReason: "trace_size_limit", OriginalSize: 99})
	if !got.Redacted || got.RedactionReason != "trace_size_limit" || got.OriginalSize != 99 || got.Size != 0 {
		t.Fatalf("omission metadata changed: %+v", got)
	}
}
