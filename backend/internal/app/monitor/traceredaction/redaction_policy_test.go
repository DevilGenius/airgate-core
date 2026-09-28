package traceredaction

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestStructuredRedactionUsesOneFieldPolicy(t *testing.T) {
	for _, names := range [][]string{credentialFields, imageFields} {
		for _, name := range names {
			for _, format := range []string{"json", "form", "multipart"} {
				t.Run(format+"/"+name, func(t *testing.T) {
					const secret = "private_payload_937"
					var raw []byte
					contentType := "application/json"
					switch format {
					case "json":
						raw, _ = json.Marshal(map[string]any{name: secret, "keep": "visible"})
					case "form":
						raw = []byte(url.Values{name: {secret}, "keep": {"visible"}}.Encode())
						contentType = "application/x-www-form-urlencoded"
					case "multipart":
						var buf bytes.Buffer
						writer := multipart.NewWriter(&buf)
						_ = writer.WriteField(name, secret)
						_ = writer.WriteField("keep", "visible")
						_ = writer.Close()
						raw = buf.Bytes()
						contentType = writer.FormDataContentType()
					}
					before := bytes.Clone(raw)
					result := SanitizeBody(raw, contentType, BodyOptions{})
					if !result.Redacted || result.OriginalSize != int64(len(raw)) || bytes.Contains(result.Body, []byte(secret)) || !bytes.Contains(result.Body, []byte("visible")) {
						t.Fatalf("policy differed across formats: %+v", result)
					}
					if !bytes.Equal(raw, before) {
						t.Fatal("sanitizer changed forwarding bytes")
					}
				})
			}
		}
	}
}

func TestMultipartDropsFilesAndRedactsCredentials(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("password", "multipart_secret")
	_ = writer.WriteField("prompt", "ordinary prompt")
	file, err := writer.CreateFormFile("attachment", "document.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("private_binary_bytes"))
	_ = writer.Close()
	result := SanitizeBody(buf.Bytes(), writer.FormDataContentType(), BodyOptions{})
	if !result.Redacted || result.ContentType != "application/json" || bytes.Contains(result.Body, []byte("multipart_secret")) || bytes.Contains(result.Body, []byte("private_binary_bytes")) || !bytes.Contains(result.Body, []byte("ordinary prompt")) {
		t.Fatalf("multipart rules inconsistent: %+v", result)
	}
}

func TestMalformedSensitiveJSONIsNotStored(t *testing.T) {
	for _, raw := range []string{`{"ACCESS-TOKEN":"secret`, `{" B64_JSON ":"image`, `{"api_\u006bey":"secret`, `{"type":"input_image","data":"private`, `{"type":"image_generation_call","result":"private`} {
		result := SanitizeBody([]byte(raw), "application/json", BodyOptions{})
		if !result.Redacted || len(result.Body) != 0 || result.OriginalSize != int64(len(raw)) {
			t.Fatalf("unsafe malformed input retained: %+v", result)
		}
	}
}

func TestOrdinaryPayloadPreservesFormattingAndTypedResults(t *testing.T) {
	ordinary := []byte("{\n \"email\":\"person@example.test\", \"result\":\"ordinary tool result\", \"encrypted_content\":\"opaque\", \"token\":12, \"number\":9007199254740993\n}")
	got := SanitizeBody(ordinary, "application/json", BodyOptions{})
	if got.Redacted || !bytes.Equal(got.Body, ordinary) {
		t.Fatal("ordinary body was rewritten")
	}
	generated := []byte(`{"output":[{"type":"image_generation_call","id":"img","result":"private_image_result"},{"type":"function_call_output","result":"keep-tool-result"}]}`)
	got = SanitizeBody(generated, "application/json", BodyOptions{})
	if !got.Redacted || bytes.Contains(got.Body, []byte("private_image_result")) || !bytes.Contains(got.Body, []byte("keep-tool-result")) || !bytes.Contains(got.Body, []byte("img")) {
		t.Fatal("image result rule affected unrelated tool output")
	}
}

func TestHeaderTrustBoundaryAndSharedSanitization(t *testing.T) {
	forged := "0123456789abcdef0123456789abcdef"
	headers := http.Header{"Authorization": {"secret"}, "Cookie": {"secret"}, "X-Api-Key": {"secret"}, "User-Agent": {"fixture"}, "Retry-After": {"3"}, "X-Airgate-Trace-Session-Id-Xxh3-128": {forged}, "X-Airgate-Trace-Unknown": {"private"}}
	if got := SanitizeHeaders(headers); got.Get("X-Airgate-Trace-Session-Id-Xxh3-128") != "" || got.Get("Authorization") != "" || got.Get("Cookie") != "" || got.Get("X-Airgate-Trace-Unknown") != "" {
		t.Fatalf("untrusted metadata passed ingress: %v", got)
	}
	headers.Set("session_id", "actual-session")
	safe := SanitizeHeaders(headers)
	if safe.Get("X-Airgate-Trace-Session-Id-Xxh3-128") != HashString("actual-session") || safe.Get("session_id") != "" || safe.Get("Retry-After") != "3" {
		t.Fatalf("header policy mismatch: %v", safe)
	}
	invalid := SanitizeHeaders(http.Header{"X-Airgate-Trace-Session-Id-Xxh3-128": {"not-a-fingerprint"}})
	if len(invalid) != 0 {
		t.Fatal("invalid fingerprint accepted")
	}
}

func TestSharedURLAndSummaryRedaction(t *testing.T) {
	for _, input := range []string{"https://user:password@example.test/path?token=secret#private", "wss://user:password@example.test/path?token=secret#private", "%bad"} {
		got := SanitizeURL(input)
		if strings.Contains(got, "password") || strings.Contains(got, "secret") || strings.Contains(got, "private") || SanitizeURL(got) != got {
			t.Fatalf("URL redaction is unsafe or not idempotent: %q", got)
		}
	}
	input := `Bearer bearer_secret sk-1234567890 api_key="api secret" password='password secret' client-secret=client_secret_value user@example.test b64_json="image_secret" data:image/png;base64,aGVsbG8= https://user:pw@example.test/path?sig=query_secret`
	got := SanitizeText(input)
	for _, secret := range []string{"bearer_secret", "1234567890", "api secret", "password secret", "client_secret_value", "user@example.test", "image_secret", "aGVsbG8=", "query_secret", "user:pw"} {
		if strings.Contains(got, secret) {
			t.Fatalf("summary leaked %q: %s", secret, got)
		}
	}
	if SanitizeText(got) != got {
		t.Fatalf("summary redaction is not idempotent: %s", got)
	}
}

func TestDiagnosticDetailFieldPolicy(t *testing.T) {
	for _, key := range []string{"api_key", "apiKey", "headers_authorization", "access-token", "private-key", "session_id", "password", "nested_client_secret"} {
		if !IsSensitiveDetailKey(key) {
			t.Fatalf("sensitive detail key retained: %s", key)
		}
	}
	if IsSensitiveDetailKey("account_id") || IsSensitiveDetailKey("elapsed_ms") {
		t.Fatal("ordinary diagnostic metadata was masked")
	}
}
