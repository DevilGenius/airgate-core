package traceredaction

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestImageOutputFieldsAreRedacted(t *testing.T) {
	const encoded = "cHJpdmF0ZS1pbWFnZS1ieXRlcw=="
	for _, field := range []string{"b64_json", "partial_image_b64", "partial_image", "image_b64"} {
		for _, key := range []string{field, strings.ToUpper(field)} {
			t.Run(key, func(t *testing.T) {
				raw, err := json.Marshal(map[string]any{"type": "response.image_generation_call.partial_image", "item_id": "img_fixture", "data": []any{map[string]any{key: encoded, "revised_prompt": "keep"}}})
				if err != nil {
					t.Fatal(err)
				}
				snapshot := SanitizeBody(raw, "application/json", BodyOptions{})
				if !snapshot.Redacted || snapshot.RedactionReason != "image_input" || snapshot.OriginalSize != int64(len(raw)) {
					t.Fatalf("missing redaction metadata: %+v", snapshot)
				}
				if bytes.Contains(snapshot.Body, []byte(encoded)) || bytes.Contains(snapshot.Body, []byte("\""+key+"\":")) {
					t.Fatalf("image bytes persisted: %s", snapshot.Body)
				}
				if !bytes.Contains(snapshot.Body, []byte("img_fixture")) || !bytes.Contains(snapshot.Body, []byte("keep")) {
					t.Fatal("unrelated event metadata was removed")
				}
			})
		}
		t.Run(field+"/truncated", func(t *testing.T) {
			raw := []byte(`{"` + field + `":"` + encoded)
			snapshot := SanitizeBody(raw, "application/json", BodyOptions{})
			if !snapshot.Redacted || len(snapshot.Body) != 0 || snapshot.OriginalSize != int64(len(raw)) {
				t.Fatal("truncated image payload was retained")
			}
		})
	}
}

func TestImagePartialEventAtTraceLimitDoesNotPersistBase64(t *testing.T) {
	prefix := `{"type":"response.image_generation_call.partial_image","item_id":"img_fixture","partial_image_b64":"`
	suffix := `","partial_image_index":1}`
	raw := []byte(prefix + strings.Repeat("A", (16<<20)-len(prefix)-len(suffix)) + suffix)
	before := Hash(raw)
	body := SanitizeBody(raw, "application/json", BodyOptions{}).Body
	if len(body) == 0 || len(body) > 1024 || bytes.Contains(body, []byte("partial_image_b64")) {
		t.Fatalf("partial image reached stored diagnostic: length=%d", len(body))
	}
	var event map[string]any
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatal(err)
	}
	if event["item_id"] != "img_fixture" || event["type"] != "response.image_generation_call.partial_image" || event["partial_image_index"] != float64(1) {
		t.Fatal("event metadata was not preserved")
	}
	if Hash(raw) != before {
		t.Fatal("redaction modified the forwarded event")
	}
}
