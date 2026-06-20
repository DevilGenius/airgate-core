package account

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

type accountFakeGatewayPlugin struct {
	platform string
	models   []sdk.ModelInfo

	forward func(context.Context, *sdk.ForwardRequest) (sdk.ForwardOutcome, error)
	handle  func(context.Context, string, string, string, http.Header, []byte) (int, http.Header, []byte, error)
}

func (p *accountFakeGatewayPlugin) Info() sdk.PluginInfo {
	return sdk.PluginInfo{ID: "fake-" + p.platform, Name: "Fake " + p.platform, Type: sdk.PluginTypeGateway}
}

func (p *accountFakeGatewayPlugin) Init(sdk.PluginContext) error     { return nil }
func (p *accountFakeGatewayPlugin) Start(context.Context) error      { return nil }
func (p *accountFakeGatewayPlugin) Stop(context.Context) error       { return nil }
func (p *accountFakeGatewayPlugin) Platform() string                 { return p.platform }
func (p *accountFakeGatewayPlugin) Models() []sdk.ModelInfo          { return p.models }
func (p *accountFakeGatewayPlugin) Routes() []sdk.RouteDefinition    { return nil }
func (p *accountFakeGatewayPlugin) ValidateAccount(context.Context, map[string]string) error {
	return nil
}

func (p *accountFakeGatewayPlugin) Forward(ctx context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
	if p.forward == nil {
		return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}, nil
	}
	return p.forward(ctx, req)
}

func (p *accountFakeGatewayPlugin) HandleWebSocket(context.Context, sdk.WebSocketConn) (sdk.ForwardOutcome, error) {
	return sdk.ForwardOutcome{}, sdk.ErrNotSupported
}

func (p *accountFakeGatewayPlugin) HandleRequest(ctx context.Context, method, path, query string, headers http.Header, body []byte) (int, http.Header, []byte, error) {
	if p.handle == nil {
		return http.StatusNotFound, nil, nil, nil
	}
	return p.handle(ctx, method, path, query, headers, body)
}

type accountGatewayRuntime struct {
	instance *plugin.PluginInstance
	cleanup  func()
}

func newAccountGatewayRuntime(t *testing.T, impl *accountFakeGatewayPlugin) accountGatewayRuntime {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	if err := (&sdkgrpc.GatewayGRPCPlugin{Impl: impl}).GRPCServer(nil, server); err != nil {
		t.Fatalf("register gateway gRPC server: %v", err)
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("gateway gRPC server stopped with error: %v", err)
		}
	}()

	ctx := t.Context()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial gateway gRPC server: %v", err)
	}

	rawClient, err := (&sdkgrpc.GatewayGRPCPlugin{}).GRPCClient(ctx, nil, conn)
	if err != nil {
		t.Fatalf("create gateway gRPC client: %v", err)
	}
	gateway, ok := rawClient.(*sdkgrpc.GatewayGRPCClient)
	if !ok {
		t.Fatalf("gateway client type = %T", rawClient)
	}

	return accountGatewayRuntime{
		instance: &plugin.PluginInstance{Name: impl.Info().ID, Platform: impl.platform, Gateway: gateway},
		cleanup: func() {
			_ = conn.Close()
			server.Stop()
			_ = listener.Close()
		},
	}
}

type accountGatewayCatalog struct {
	stubPluginCatalog
	instances map[string]*plugin.PluginInstance
}

func (c accountGatewayCatalog) GetPluginByPlatform(platform string) *plugin.PluginInstance {
	return c.instances[platform]
}

func TestQuotaRefreshThroughGatewayPersistsCredentialsAndUsage(t *testing.T) {
	var mu sync.Mutex
	requestedPaths := make([]string, 0, 2)
	savedCredentials := map[string]string{}
	var quotaRequest quotaRefreshRequest
	var probeRequest map[string]any

	runtime := newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
		platform: "openai",
		handle: func(_ context.Context, method, path, _ string, _ http.Header, body []byte) (int, http.Header, []byte, error) {
			if method != http.MethodPost {
				t.Fatalf("method = %s, want POST", method)
			}
			mu.Lock()
			requestedPaths = append(requestedPaths, path)
			mu.Unlock()
			switch path {
			case "accounts/quota":
				if err := json.Unmarshal(body, &quotaRequest); err != nil {
					t.Fatalf("quota request body: %v", err)
				}
				return http.StatusOK, nil, []byte(`{
					"expires_at":"2026-07-01T00:00:00Z",
					"extra":{"plan_type":"plus","email":"user@example.test","refresh_warning":"soon","empty_ignored":""}
				}`), nil
			case "usage/probe":
				if err := json.Unmarshal(body, &probeRequest); err != nil {
					t.Fatalf("probe request body: %v", err)
				}
				return http.StatusOK, nil, []byte(`{"credits":{"balance":12.5},"windows":[{"key":"5h","used_percent":30,"reset_after_seconds":3600}]}`), nil
			default:
				return http.StatusNotFound, nil, nil, nil
			}
		},
	})
	defer runtime.cleanup()

	writer := newStubStateWriter()
	service := NewService(stubRepository{
		findByID: func(_ context.Context, id int, opts LoadOptions) (Account, error) {
			if id != 9 || !opts.WithProxy {
				t.Fatalf("FindByID id=%d opts=%+v", id, opts)
			}
			return Account{
				ID:          id,
				Name:        "oauth",
				Platform:    "openai",
				Type:        "oauth",
				Credentials: map[string]string{"access_token": "old", "plan_type": "free"},
				Proxy:       &Proxy{Protocol: "http", Address: "127.0.0.1", Port: 65530},
			}, nil
		},
		saveCredentials: func(_ context.Context, id int, credentials map[string]string) error {
			if id != 9 {
				t.Fatalf("SaveCredentials id = %d, want 9", id)
			}
			savedCredentials = cloneStringMap(credentials)
			return nil
		},
	}, accountGatewayCatalog{instances: map[string]*plugin.PluginInstance{"openai": runtime.instance}}, nil, writer)
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	got, err := service.RefreshQuota(t.Context(), 9)
	if err != nil {
		t.Fatalf("RefreshQuota() error = %v", err)
	}
	if got.PlanType != "plus" || got.Email != "user@example.test" ||
		got.SubscriptionActiveUntil != "2026-07-01T00:00:00Z" || got.ReauthWarning != "soon" {
		t.Fatalf("RefreshQuota() = %+v", got)
	}
	if savedCredentials["plan_type"] != "plus" || savedCredentials["email"] != "user@example.test" ||
		savedCredentials["subscription_active_until"] != "2026-07-01T00:00:00Z" ||
		savedCredentials["refresh_warning"] != "" || savedCredentials["empty_ignored"] != "" {
		t.Fatalf("saved credentials = %+v", savedCredentials)
	}
	if quotaRequest.Credentials["proxy_url"] == "" {
		t.Fatalf("quota request credentials should include proxy_url: %+v", quotaRequest.Credentials)
	}
	if probeRequest["id"].(float64) != 9 {
		t.Fatalf("probe request = %+v", probeRequest)
	}
	if !writer.routeRefreshed[9] || writer.markersCleared[9] == 0 {
		t.Fatalf("state writer refreshed=%+v markersCleared=%+v", writer.routeRefreshed, writer.markersCleared)
	}
	if info, ok := service.getUsageInfoForAccount(t.Context(), 9); !ok || info.Credits == nil || info.Credits.Balance != 12.5 {
		t.Fatalf("usage cache info=%+v ok=%v", info, ok)
	}
	mu.Lock()
	paths := strings.Join(requestedPaths, ",")
	mu.Unlock()
	if paths != "accounts/quota,usage/probe" {
		t.Fatalf("requested paths = %s", paths)
	}
}

func TestQuotaRefreshGatewayErrorsAndWarnings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{name: "unsupported", statusCode: http.StatusNotFound, wantErr: ErrQuotaRefreshUnsupported},
		{name: "reauth status", statusCode: http.StatusForbidden, body: `{"error_code":"other"}`, wantErr: ErrReauthRequired},
		{name: "reauth body", statusCode: http.StatusOK, body: `{"error_code":"reauth_required"}`, wantErr: ErrReauthRequired},
		{name: "http error", statusCode: http.StatusBadGateway, body: `{}`, wantErr: nil},
		{name: "bad json", statusCode: http.StatusOK, body: `{bad`, wantErr: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
				platform: "openai",
				handle: func(context.Context, string, string, string, http.Header, []byte) (int, http.Header, []byte, error) {
					return tc.statusCode, nil, []byte(tc.body), nil
				},
			})
			defer runtime.cleanup()

			recorder := &captureMonitorRecorder{}
			service := NewService(stubRepository{}, accountGatewayCatalog{instances: map[string]*plugin.PluginInstance{"openai": runtime.instance}}, nil, nil)
			service.SetMonitorRecorder(recorder)

			_, err := service.refreshQuota(t.Context(), Account{
				ID: 7, Name: "oauth", Platform: "openai", Type: "oauth", Credentials: map[string]string{"access_token": "tok"},
			}, false)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("refreshQuota() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err == nil {
				t.Fatal("refreshQuota() should fail")
			}
			if len(recorder.records) != 1 {
				t.Fatalf("monitor records = %+v, want one failure", recorder.records)
			}
		})
	}
}

func TestUsageProbeThroughGatewayBatchAndFallback(t *testing.T) {
	var seenBodies [][]byte
	runtime := newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
		platform: "openai",
		handle: func(_ context.Context, _ string, path, _ string, _ http.Header, body []byte) (int, http.Header, []byte, error) {
			seenBodies = append(seenBodies, append([]byte(nil), body...))
			switch path {
			case "usage/accounts":
				return http.StatusOK, nil, []byte(`{
					"accounts":{
						"1":{"credits":{"balance":5},"windows":[{"key":"5h","used_percent":20,"reset_after_seconds":1200}]},
						"2":{"credits":{"balance":7}},
						"999":{"credits":{"balance":99}}
					},
					"errors":[{"id":1,"message":"HTTP 401 token expired"},{"id":2,"message":"disabled ignored"},{"id":3,"message":"pool ignored"}]
				}`), nil
			case "usage/probe":
				return http.StatusOK, nil, []byte(`{"errors":[{"id":4,"message":"probe soft error"}]}`), nil
			default:
				return http.StatusNotFound, nil, nil, nil
			}
		},
	})
	defer runtime.cleanup()

	writer := newStubStateWriter()
	service := NewService(stubRepository{}, accountGatewayCatalog{instances: map[string]*plugin.PluginInstance{"openai": runtime.instance}}, nil, writer)
	service.now = func() time.Time { return time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC) }

	usage, err := service.fetchUpstreamUsageForAccounts(t.Context(), []Account{
		{ID: 1, Platform: "openai", Type: "oauth", State: "active", Credentials: map[string]string{"access_token": "a"}},
		{ID: 2, Platform: "openai", Type: "oauth", State: "disabled", Credentials: map[string]string{"access_token": "b"}},
		{ID: 3, Platform: "openai", Type: "oauth", State: "active", UpstreamIsPool: true, Credentials: map[string]string{"access_token": "c"}},
		{ID: 4, Platform: "openai", Type: "apikey", State: "active", Credentials: map[string]string{"api_key": "sk"}},
		{ID: 5, Platform: "", Type: "oauth", State: "active", Credentials: map[string]string{"access_token": "d"}},
	})
	if err != nil {
		t.Fatalf("fetchUpstreamUsageForAccounts() error = %v", err)
	}
	if len(usage) != 3 || usage["1"].Credits == nil || usage["1"].Credits.Balance != 5 ||
		usage["3"].Credits != nil || usage["5"].Credits != nil || usage["999"].Credits != nil {
		t.Fatalf("usage = %+v", usage)
	}
	if writer.disabled[1] == "" || writer.disabled[2] != "" || writer.disabled[3] != "" {
		t.Fatalf("disabled markers = %+v", writer.disabled)
	}
	if len(seenBodies) == 0 || !strings.Contains(string(seenBodies[0]), `"id":1`) ||
		strings.Contains(string(seenBodies[0]), `"id":2`) || strings.Contains(string(seenBodies[0]), `"id":4`) {
		t.Fatalf("usage/accounts body = %s", string(seenBodies[0]))
	}

	info, usageErrors, ok := service.fetchSingleAccountUsage(t.Context(), Account{ID: 4, Platform: "openai", Type: "oauth"})
	if ok || len(usageErrors) != 1 || info.Credits != nil {
		t.Fatalf("fetchSingleAccountUsage probe-only errors info=%+v errors=%+v ok=%v", info, usageErrors, ok)
	}

	runtime.cleanup()
	runtime = newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
		platform: "openai",
		handle: func(_ context.Context, _ string, path, _ string, _ http.Header, _ []byte) (int, http.Header, []byte, error) {
			if path == "usage/probe" {
				return http.StatusBadGateway, nil, nil, nil
			}
			return http.StatusOK, nil, []byte(`{"accounts":{"4":{"credits":{"balance":8}}}}`), nil
		},
	})
	defer runtime.cleanup()
	service = NewService(stubRepository{}, accountGatewayCatalog{instances: map[string]*plugin.PluginInstance{"openai": runtime.instance}}, nil, nil)
	info, usageErrors, ok = service.fetchSingleAccountUsage(t.Context(), Account{ID: 4, Platform: "openai", Type: "oauth"})
	if !ok || len(usageErrors) != 0 || info.Credits == nil || info.Credits.Balance != 8 {
		t.Fatalf("fetchSingleAccountUsage fallback info=%+v errors=%+v ok=%v", info, usageErrors, ok)
	}
}

func TestPrepareConnectivityTestRunsGatewayOutcomes(t *testing.T) {
	recorder := &captureMonitorRecorder{}
	runtime := newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
		platform: "openai",
		models:   []sdk.ModelInfo{{ID: "gpt-test"}},
		forward: func(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			if req.Account.ID != 11 || req.Model != "gpt-test" || !req.Stream || req.Headers.Get("X-Airgate-Internal") != "test" {
				t.Fatalf("forward request = %+v headers=%v", req, req.Headers)
			}
			if req.Writer == nil {
				t.Fatal("forward writer should be set")
			}
			req.Writer.WriteHeader(http.StatusAccepted)
			_, _ = req.Writer.Write([]byte("streamed"))
			return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}, nil
		},
	})
	defer runtime.cleanup()

	service := NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{ID: 11, Name: "oauth", Platform: "openai", Type: "oauth", Credentials: map[string]string{"access_token": "tok"}}, nil
		},
	}, accountGatewayCatalog{
		stubPluginCatalog: stubPluginCatalog{models: []sdk.ModelInfo{{ID: "gpt-test"}}},
		instances:         map[string]*plugin.PluginInstance{"openai": runtime.instance},
	}, nil, nil)
	service.SetMonitorRecorder(recorder)

	ct, err := service.PrepareConnectivityTest(t.Context(), 11, "")
	if err != nil {
		t.Fatalf("PrepareConnectivityTest() error = %v", err)
	}
	rr := httptest.NewRecorder()
	if err := ct.Run(t.Context(), rr); err != nil {
		t.Fatalf("ConnectivityTest.Run() error = %v", err)
	}
	if rr.Code != http.StatusAccepted || rr.Body.String() != "streamed" {
		t.Fatalf("recorder code=%d body=%q", rr.Code, rr.Body.String())
	}
	if len(recorder.resolved) != 1 || recorder.resolved[0].SubjectID != "11" {
		t.Fatalf("resolved monitor events = %+v", recorder.resolved)
	}

	runtime.cleanup()
	runtime = newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
		platform: "openai",
		models:   []sdk.ModelInfo{{ID: "gpt-test"}},
		forward: func(context.Context, *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			return sdk.ForwardOutcome{
				Kind:     sdk.OutcomeClientError,
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"message":"bad model"}}`)},
			}, nil
		},
	})
	defer runtime.cleanup()

	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{ID: 12, Name: "oauth", Platform: "openai", Type: "oauth"}, nil
		},
	}, accountGatewayCatalog{
		stubPluginCatalog: stubPluginCatalog{models: []sdk.ModelInfo{{ID: "gpt-test"}}},
		instances:         map[string]*plugin.PluginInstance{"openai": runtime.instance},
	}, nil, nil)
	service.SetMonitorRecorder(recorder)

	ct, err = service.PrepareConnectivityTest(t.Context(), 12, "")
	if err != nil {
		t.Fatalf("PrepareConnectivityTest failure case error = %v", err)
	}
	err = ct.Run(t.Context(), httptest.NewRecorder())
	if err == nil || !strings.Contains(err.Error(), "HTTP 400: bad model") {
		t.Fatalf("ConnectivityTest.Run failure error = %v", err)
	}
	if len(recorder.records) == 0 || recorder.records[len(recorder.records)-1].SubjectID != "12" {
		t.Fatalf("monitor failure records = %+v", recorder.records)
	}
}
