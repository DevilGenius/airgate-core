package traceredaction

import (
	"encoding/hex"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalHeaderFingerprints(t *testing.T) {
	values := []string{"session-a", "session-b"}
	for _, name := range []string{"session_id", "Session-Id", "X-SESSION-ID"} {
		fingerprints := HeaderFingerprints(http.Header{name: values})
		digest := fingerprints["x-airgate-trace-session-id-xxh3-128"]
		raw, err := hex.DecodeString(digest)
		if err != nil || len(raw) != 16 || digest != strings.ToLower(digest) || digest != HashString("session-a\x00session-b") {
			t.Fatalf("%s fingerprint=%v", name, fingerprints)
		}
	}
	if HashString("session-a\x00session-b") != Hash([]byte("session-a\x00session-b")) {
		t.Fatal("string and byte hashing differ")
	}
	a := HeaderFingerprints(http.Header{"conversation_id": {"ab", "c"}})
	b := HeaderFingerprints(http.Header{"conversation-id": {"a", "bc"}})
	if reflect.DeepEqual(a, b) {
		t.Fatal("multi-value boundaries were lost")
	}
}

func TestHeaderFingerprintAliasPrecedence(t *testing.T) {
	headers := http.Header{"session_id": {"canonical"}, "X-Session-Id": {"fallback"}, "conversation-id": {"conversation"}, "x-codex-turn-state": {"turn"}, "Authorization": {"secret"}}
	expected := HashString("canonical")
	for i := 0; i < 50; i++ {
		safe := SanitizeHeaders(headers)
		if safe.Get("X-Airgate-Trace-Session-Id-Xxh3-128") != expected {
			t.Fatal("alias resolution depends on map order")
		}
		for key := range safe {
			if strings.Contains(strings.ToLower(key), "sha256") {
				t.Fatal("legacy fingerprint algorithm emitted")
			}
		}
	}
}
