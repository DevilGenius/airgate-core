package monitor

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/DevilGenius/airgate-sdk/runtimego/requesttrace"
)

func TestIngressAndOutboundTraceFingerprintsMatch(t *testing.T) {
	ingress := http.Header{"X-Session-Id": {"same-session", "second-value"}, "Conversation-Id": {"same-conversation"}, "X-Codex-Turn-State": {"same-turn"}}
	outbound := http.Header{"session_id": {"same-session", "second-value"}, "conversation_id": {"same-conversation"}, "x-codex-turn-state": {"same-turn"}}
	before := safeStoredTraceHeaders(ingress)
	after := safeStoredTraceHeaders(requesttrace.SafeHeaders(outbound))
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("ingress=%v outbound=%v", before, after)
	}
	for _, key := range []string{"x-airgate-trace-session-id-xxh3-128", "x-airgate-trace-conversation-id-xxh3-128", "x-airgate-trace-x-codex-turn-state-xxh3-128"} {
		if len(before[key]) != 1 || len(before[key][0]) != 32 {
			t.Fatalf("invalid canonical fingerprint %s: %v", key, before)
		}
	}
}
