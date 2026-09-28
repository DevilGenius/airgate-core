package traceredaction

import (
	"bytes"
	"strings"
	"testing"
)

func TestSharedBodyRedaction(t *testing.T) {
	for _, body := range []string{`{"access_token":"secret","nested":{"refresh_token":"secret"}}`, "client_secret=secret&grant_type=refresh_token"} {
		contentType := "application/json"
		if strings.HasPrefix(body, "client_secret") {
			contentType = "application/x-www-form-urlencoded"
		}
		snapshot := SanitizeBody([]byte(body), contentType, BodyOptions{})
		if !snapshot.Redacted || snapshot.RedactionReason != "credentials" || snapshot.OriginalSize != int64(len(body)) || strings.Contains(string(snapshot.Body), `:"secret"`) || strings.Contains(string(snapshot.Body), "=secret") {
			t.Fatalf("credential redaction failed: %+v", snapshot)
		}
	}
	body := []byte(`{"input":[{"type":"input_text","text":"keep"},{"type":"input_image","image_url":"data:image/png;base64,c2VjcmV0"}]}`)
	snapshot := SanitizeBody(body, "application/json", BodyOptions{})
	if !snapshot.Redacted || snapshot.RedactionReason != "image_input" || !bytes.Contains(snapshot.Body, []byte("keep")) || bytes.Contains(snapshot.Body, []byte("c2VjcmV0")) {
		t.Fatal("image redaction failed")
	}
}
