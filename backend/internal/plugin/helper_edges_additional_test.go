package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestUsageMetadataEdgeBranches(t *testing.T) {
	t.Parallel()

	meta := map[string]string{
		"bad":   "not-a-number",
		"blank": " ",
		"float": "12.7",
	}
	if got := metadataInt(meta, "bad", "blank", "float"); got != 12 {
		t.Fatalf("metadataInt fallback = %d, want 12", got)
	}
	if got := metadataInt(meta, "bad"); got != 0 {
		t.Fatalf("metadataInt invalid = %d, want 0", got)
	}

	out := map[string]string{}
	putMetadataFloat(out, "zero", 0)
	putMetadataFloat(out, "negative", -1)
	putMetadataFloat(out, "positive", 1.25)
	if _, ok := out["zero"]; ok {
		t.Fatal("zero metadata float should be omitted")
	}
	if _, ok := out["negative"]; ok {
		t.Fatal("negative metadata float should be omitted")
	}
	if got := out["positive"]; got != "1.25" {
		t.Fatalf("positive metadata float = %q, want 1.25", got)
	}
}

func TestOutcomeExtractionAndSanitizationEdges(t *testing.T) {
	t.Parallel()

	if got := sanitizedClientErrorStatus(sdk.ForwardOutcome{
		Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway},
	}); got != http.StatusBadRequest {
		t.Fatalf("sanitizedClientErrorStatus = %d, want %d", got, http.StatusBadRequest)
	}

	if got := extractErrorMessage([]byte(`{"error":"plain error"}`)); got != "plain error" {
		t.Fatalf("string error message = %q", got)
	}
	for _, body := range [][]byte{
		nil,
		[]byte(`not-json`),
		[]byte(`{"error":{"code":"invalid"}}`),
	} {
		if got := extractErrorMessage(body); got != "" {
			t.Fatalf("extractErrorMessage(%q) = %q, want empty", string(body), got)
		}
	}
	for _, body := range [][]byte{
		nil,
		[]byte(`not-json`),
		[]byte(`{"error":"plain error"}`),
		[]byte(`{"error":{"message":"missing code"}}`),
	} {
		if got := extractErrorCode(body); got != "" {
			t.Fatalf("extractErrorCode(%q) = %q, want empty", string(body), got)
		}
	}

	tests := []struct {
		kind sdk.OutcomeKind
		want string
	}{
		{sdk.OutcomeAccountRateLimited, "上游账号当前被限流，请稍后重试"},
		{sdk.OutcomeAccountDead, "上游账号不可用，请联系管理员"},
		{sdk.OutcomeAccountUnavailable, "上游账号403暂不可用，请稍后重试"},
		{sdk.OutcomeStreamAborted, "响应流中断"},
		{sdk.OutcomeUpstreamTransient, "上游服务暂不可用，请稍后重试"},
		{sdk.OutcomeUnknown, "上游服务暂不可用，请稍后重试"},
	}
	for _, tt := range tests {
		if got := sanitizedMessage(tt.kind); got != tt.want {
			t.Fatalf("sanitizedMessage(%v) = %q, want %q", tt.kind, got, tt.want)
		}
	}

	unsafeTypes := []string{
		"text/html; charset=utf-8",
		"application/xhtml+xml",
		"image/svg+xml",
		"application/javascript",
		"text/ecmascript",
	}
	for _, contentType := range unsafeTypes {
		if !isUnsafeUpstreamContentType(contentType) {
			t.Fatalf("%q should be unsafe", contentType)
		}
	}
	for _, contentType := range []string{"", "application/json; charset=utf-8", "text/plain"} {
		if isUnsafeUpstreamContentType(contentType) {
			t.Fatalf("%q should be safe", contentType)
		}
	}
}

func TestWriteUpstreamFiltersHeadersAndDefaults(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writeUpstream(c, sdk.UpstreamResponse{
		Headers: http.Header{
			"Content-Type": {"application/json"},
			"Set-Cookie":   {"session=secret"},
			"X-Powered-By": {"plugin"},
			"X-Trace":      {"first", "last"},
		},
		Body: []byte(`{"ok":true}`),
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != `{"ok":true}` {
		t.Fatalf("body = %q", got)
	}
	if got := recorder.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("Set-Cookie = %q, want empty", got)
	}
	if got := recorder.Header().Get("X-Powered-By"); got != "" {
		t.Fatalf("X-Powered-By = %q, want empty", got)
	}
	if got := recorder.Header().Get("X-Trace"); got != "last" {
		t.Fatalf("X-Trace = %q, want last", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestWriteUpstreamRejectsUnsafeContentType(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writeUpstream(c, sdk.UpstreamResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": {"text/html"}},
		Body:       []byte(`<script>alert(1)</script>`),
	})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if got := recorder.Body.String(); got == `<script>alert(1)</script>` {
		t.Fatal("unsafe upstream body should not be passed through")
	}
}

func TestRequestTransportAndSessionEdges(t *testing.T) {
	t.Parallel()

	if got := requestTransportMethod(nil); got != http.MethodGet {
		t.Fatalf("nil request method = %q, want GET", got)
	}
	req := httptest.NewRequest("", "/v1/responses", nil)
	req.Method = ""
	if got := requestTransportMethod(req); got != http.MethodGet {
		t.Fatalf("blank request method = %q, want GET", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	req.Header.Set("Upgrade", "WebSocket")
	req.Header.Set("Connection", "keep-alive, Upgrade")
	if got := requestTransportMethod(req); got != "WS" {
		t.Fatalf("websocket method = %q, want WS", got)
	}
	req.Header.Set("Connection", "keep-alive")
	if isWebSocketUpgrade(req) {
		t.Fatal("websocket upgrade should require Connection: upgrade")
	}

	if got := resolveRequestSessionID(nil, parsedRequest{SessionID: "direct-session"}); got != "direct-session" {
		t.Fatalf("direct session = %q", got)
	}
	headers := http.Header{"Conversation-Id": {"conv-header"}}
	if got := resolveRequestSessionID(headers, parsedRequest{}); got != "conversation:conv-header" {
		t.Fatalf("header conversation = %q", got)
	}
	if got := sessionIDFromMetadataUserID(`{"session_id":`); got != "" {
		t.Fatalf("invalid metadata JSON session = %q, want empty", got)
	}
	if got := sessionIDFromMetadataUserID("user_session_ tail "); got != "claude:tail" {
		t.Fatalf("legacy session = %q, want claude:tail", got)
	}
}

func TestTaskAssetObjectKeyCollectionEdges(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	addStringValues(seen, "missing", nil)
	addStringValues(seen, "missing", map[string]interface{}{"other": "ignored"})
	addStringValues(seen, "assets", map[string]interface{}{
		"assets": map[string]any{
			"nested": []any{"generated/a.png", "", []string{"generated/b.png"}},
		},
	})
	if _, ok := seen["generated/a.png"]; !ok {
		t.Fatal("nested string asset key was not collected")
	}
	if _, ok := seen["generated/b.png"]; !ok {
		t.Fatal("nested []string asset key was not collected")
	}

	collectRuntimeAssetObjectKeys(seen, map[string]any{
		"text": "see /assets-runtime/images/a%2Fb.png?download=1 and /assets-runtime/%zz",
		"list": []string{"/assets-runtime/tasks/c.png"},
	})
	if _, ok := seen["images/a/b.png"]; !ok {
		t.Fatal("escaped runtime asset key was not collected")
	}
	if _, ok := seen["tasks/c.png"]; !ok {
		t.Fatal("runtime asset key from []string was not collected")
	}
	if _, ok := seen["%zz"]; ok {
		t.Fatal("invalid escaped runtime asset URL should be ignored")
	}
}

func TestBuildProxyURLWithoutAndWithCredentials(t *testing.T) {
	t.Parallel()

	acc := &ent.Account{Edges: ent.AccountEdges{Proxy: &ent.Proxy{
		Protocol: "http",
		Address:  "127.0.0.1",
		Port:     8080,
	}}}
	if got := buildProxyURL(acc); got != "http://127.0.0.1:8080" {
		t.Fatalf("proxy URL = %q", got)
	}
	acc.Edges.Proxy.Username = "user"
	acc.Edges.Proxy.Password = "pass"
	if got := buildProxyURL(acc); got != "http://user:pass@127.0.0.1:8080" {
		t.Fatalf("proxy URL with credentials = %q", got)
	}
}
