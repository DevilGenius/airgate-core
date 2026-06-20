package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestNewForwarderAndRuntimePureHelpers(t *testing.T) {
	forwarder := NewForwarder(nil, nil, nil, nil, nil, nil)
	if forwarder == nil || forwarder.credentialPersistSem == nil {
		t.Fatalf("NewForwarder = %+v", forwarder)
	}

	if got := sdkCapabilitiesToStrings(nil); got != nil {
		t.Fatalf("empty capabilities = %v, want nil", got)
	}
	got := sdkCapabilitiesToStrings([]sdk.Capability{
		sdk.CapabilityHostInvoke,
		sdk.CapabilityForHostMethod("tasks.update"),
	})
	if len(got) != 2 || got[0] != "host.invoke" || got[1] != "host.invoke.tasks.update" {
		t.Fatalf("capability strings = %v", got)
	}

	manager := NewManager(t.TempDir(), "debug", "host=localhost dbname=airgate", nil)
	cfg := manager.buildInitConfig(context.Background(), "demo")
	if cfg[sdk.ConfigKeyLogLevel] != "debug" || cfg["db_dsn"] != "host=localhost dbname=airgate" {
		t.Fatalf("init config = %+v", cfg)
	}

	host := (&HostService{}).NewPluginHandle("demo")
	clientConfig := manager.newPluginClientConfig(exec.Command("airgate-plugin-test"), true, host)
	if clientConfig.SyncStdout != os.Stdout || clientConfig.SyncStderr != os.Stderr {
		t.Fatal("forwardOutput should wire plugin stdout/stderr")
	}
	if clientConfig.Plugins[sdkgrpc.PluginKeyGateway] == nil {
		t.Fatalf("gateway plugin config missing: %+v", clientConfig.Plugins)
	}
}

func TestManagerBuildInitConfigWithDatabase(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_init_config_db", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	db.Plugin.Create().
		SetName("demo").
		SetConfig(map[string]interface{}{
			sdk.ConfigKeyLogLevel: "user-log",
			"db_dsn":              "user-dsn",
			"custom":              "value",
		}).
		SaveX(ctx)
	db.Setting.Create().SetGroup("storage").SetKey("local_storage_dir").SetValue("local-assets").SaveX(ctx)
	db.Setting.Create().SetGroup("storage").SetKey("asset_s3_bucket").SetValue("bucket").SaveX(ctx)

	manager := NewManager(t.TempDir(), "info", "", db)
	t.Cleanup(manager.devWatcher.Close)
	manager.coreDSN = "host=core dbname=airgate"
	cfg := manager.buildInitConfig(ctx, "demo")
	if cfg[sdk.ConfigKeyLogLevel] != "info" || cfg["db_dsn"] != "host=core dbname=airgate" ||
		cfg["custom"] != "value" || cfg["local_storage_dir"] != "local-assets" || cfg["asset_s3_bucket"] != "bucket" {
		t.Fatalf("db init config = %+v", cfg)
	}

	closedDB := testdb.OpenMemoryEnt(t, "plugin_init_config_closed", schema.WithGlobalUniqueID(false))
	if err := closedDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	closedManager := NewManager(t.TempDir(), "debug", "", closedDB)
	t.Cleanup(closedManager.devWatcher.Close)
	closedCfg := closedManager.buildInitConfig(ctx, "demo")
	if closedCfg[sdk.ConfigKeyLogLevel] != "debug" {
		t.Fatalf("closed db init config = %+v", closedCfg)
	}
}

func TestPluginDBPureHelpers(t *testing.T) {
	fields := parseDSNFields("host=localhost port=5432 bad dbname=airgate password=pa=ss")
	if fields["host"] != "localhost" || fields["port"] != "5432" ||
		fields["dbname"] != "airgate" || fields["password"] != "pa=ss" {
		t.Fatalf("parseDSNFields = %+v", fields)
	}

	valid := []string{"a", "plugin_1", "airgate-health"}
	for _, id := range valid {
		if !isValidPluginID(id) {
			t.Fatalf("isValidPluginID(%q) = false", id)
		}
	}
	invalid := []string{"", strings.Repeat("a", 49), "bad.id", "bad id", "中文"}
	for _, id := range invalid {
		if isValidPluginID(id) {
			t.Fatalf("isValidPluginID(%q) = true", id)
		}
	}
	if got := quoteIdent(`a"b`); got != `"a""b"` {
		t.Fatalf("quoteIdent = %q", got)
	}
	if got := quoteString(`a'b`); got != `'a''b'` {
		t.Fatalf("quoteString = %q", got)
	}
	if got := quoteSearchPathIdentifier(`a"b`); got != `"a""b"` {
		t.Fatalf("quoteSearchPathIdentifier = %q", got)
	}
}

func TestHostForwardPureHelpers(t *testing.T) {
	if code := status.Code(hostForwardGenericError()); code != codes.Unavailable {
		t.Fatalf("generic error code = %v", code)
	}
	if code := status.Code(hostContextError(context.Canceled)); code != codes.Canceled {
		t.Fatalf("canceled code = %v", code)
	}
	if code := status.Code(hostContextError(context.DeadlineExceeded)); code != codes.DeadlineExceeded {
		t.Fatalf("deadline code = %v", code)
	}
	if err := hostContextError(nil); err != nil {
		t.Fatalf("nil context error = %v", err)
	}
	if code := status.Code(hostForwardInsufficientQuotaError()); code != codes.ResourceExhausted {
		t.Fatalf("quota code = %v", code)
	}
	clientErr := hostForwardClientError(sdk.ForwardOutcome{
		Kind: sdk.OutcomeClientError,
		Upstream: sdk.UpstreamResponse{
			StatusCode: http.StatusRequestEntityTooLarge,
			Body:       []byte(`{"error":{"message":"too large"}}`),
		},
	})
	if code := status.Code(clientErr); code != codes.InvalidArgument {
		t.Fatalf("client error code = %v", code)
	}

	headers := http.Header{"X-Test": {"a", "b"}}
	payload := hostForwardPayload(sdk.ForwardOutcome{
		Upstream: sdk.UpstreamResponse{StatusCode: 202, Headers: headers, Body: []byte("body")},
	})
	if payload["status_code"] != 202 || payload["body"] != "body" {
		t.Fatalf("hostForwardPayload = %+v", payload)
	}
	protoHeaders := httpHeadersToProtoHost(headers)
	if len(protoHeaders["X-Test"].([]string)) != 2 {
		t.Fatalf("httpHeadersToProtoHost = %+v", protoHeaders)
	}
	httpHeaders := protoHeadersToHTTPHost(map[string]interface{}{
		"A": []string{"1", "2"},
		"B": []interface{}{"3", 4},
		"C": map[string]interface{}{"values": []interface{}{"5", 6}},
		"D": "7",
		"E": 8,
		"F": nil,
	})
	if httpHeaders.Get("A") != "1" || httpHeaders.Values("B")[1] != "4" ||
		httpHeaders.Get("C") != "5" || httpHeaders.Get("D") != "7" ||
		httpHeaders.Get("E") != "8" || httpHeaders.Get("F") != "" {
		t.Fatalf("protoHeadersToHTTPHost = %+v", httpHeaders)
	}

	if got := hostForwardBody(nil); got != nil {
		t.Fatalf("nil body = %q", string(got))
	}
	if got := string(hostForwardBody([]byte("bytes"))); got != "bytes" {
		t.Fatalf("bytes body = %q", got)
	}
	if got := string(hostForwardBody("text")); got != "text" {
		t.Fatalf("string body = %q", got)
	}
	if got := string(hostForwardBody(json.RawMessage(`{"ok":true}`))); got != `{"ok":true}` {
		t.Fatalf("raw body = %q", got)
	}
	if got := string(hostForwardBody(map[string]any{"ok": true})); got != `{"ok":true}` {
		t.Fatalf("json body = %q", got)
	}
	if got := string(mustHostPayload(map[string]interface{}{"bad": make(chan int)})); got != `{"error":"payload encode failed"}` {
		t.Fatalf("bad host payload = %q", got)
	}
	if resp := errProbeResp("timeout", strings.Repeat("x", 600), time.Now().Add(-time.Millisecond)); resp["success"] != false ||
		!strings.HasSuffix(resp["error_msg"].(string), "...") {
		t.Fatalf("errProbeResp = %+v", resp)
	}
	if got := pickProbeModel([]sdk.ModelInfo{{ID: "image", Capabilities: []string{sdk.ModelCapImageGeneration}}, {ID: "chat"}}); got != "chat" {
		t.Fatalf("pickProbeModel = %q", got)
	}
	if got := pickProbeModel([]sdk.ModelInfo{{ID: "image", Capabilities: []string{sdk.ModelCapImageGeneration}}}); got != "" {
		t.Fatalf("pickProbeModel image only = %q", got)
	}
}

func TestHostCloneAndProxyHelpers(t *testing.T) {
	if cloneStringMap(nil) != nil || clonePluginSettings(nil) != nil || cloneOperationPolicies(nil) != nil || cloneDispatchPlansHost(nil) != nil {
		t.Fatal("nil clone helpers should return nil")
	}
	settings := clonePluginSettings(map[string]map[string]string{
		"empty": {},
		"p":     {"k": "v"},
	})
	if len(settings) != 1 || settings["p"]["k"] != "v" {
		t.Fatalf("clonePluginSettings = %+v", settings)
	}
	ops := cloneOperationPolicies(map[string]bool{"chat": true})
	ops["chat"] = false
	if !cloneOperationPolicies(map[string]bool{"chat": true})["chat"] {
		t.Fatal("cloneOperationPolicies should copy values")
	}
	plans := cloneDispatchPlansHost([]sdk.DispatchPlan{{SchedulingModel: "gpt"}})
	if len(plans) != 1 || plans[0].SchedulingModel != "gpt" {
		t.Fatalf("cloneDispatchPlansHost = %+v", plans)
	}

	if got := proxyURLFromAccount(nil); got != "" {
		t.Fatalf("nil proxy URL = %q", got)
	}
	accountWithProxy := &ent.Account{Edges: ent.AccountEdges{Proxy: &ent.Proxy{
		Protocol: "http", Address: "127.0.0.1", Port: 8080,
	}}}
	if got := proxyURLFromAccount(accountWithProxy); got != "http://127.0.0.1:8080" {
		t.Fatalf("proxyURLFromAccount = %q", got)
	}
	accountWithProxy.Edges.Proxy.Username = "user"
	accountWithProxy.Edges.Proxy.Password = "pass"
	if got := proxyURLFromAccount(accountWithProxy); got != "http://user:pass@127.0.0.1:8080" {
		t.Fatalf("proxyURLFromAccount auth = %q", got)
	}
}
