package traceredaction

import (
	"bytes"
	"testing"
)

func TestMergedRedactionPreservesCredentialAndImageRules(t *testing.T) {
	body := []byte(`{"ACCESS_TOKEN":"secret","nested":{"api_\u006bey":"other-secret"},"input":[{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="},{"type":"input_text","text":"保留文本"}],"large_number":9007199254740993}`)
	snapshot := SanitizeBody(body, "application/json", BodyOptions{})
	if !snapshot.Redacted || snapshot.RedactionReason != "image_input" || bytes.Contains(snapshot.Body, []byte("secret")) || bytes.Contains(snapshot.Body, []byte("aGVsbG8=")) || !bytes.Contains(snapshot.Body, []byte("保留文本")) || !bytes.Contains(snapshot.Body, []byte("9007199254740993")) {
		t.Fatalf("merged redaction changed contract: %s", snapshot.Body)
	}
}
