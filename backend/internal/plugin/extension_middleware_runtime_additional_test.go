package plugin

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
)

type pluginRuntimeGateway struct {
	id       string
	platform string
	forward  func(context.Context, *sdk.ForwardRequest) (sdk.ForwardOutcome, error)
}

func (p *pluginRuntimeGateway) Info() sdk.PluginInfo {
	return sdk.PluginInfo{ID: p.id, Name: p.id, Type: sdk.PluginTypeGateway}
}
func (p *pluginRuntimeGateway) Init(sdk.PluginContext) error { return nil }
func (p *pluginRuntimeGateway) Start(context.Context) error  { return nil }
func (p *pluginRuntimeGateway) Stop(context.Context) error   { return nil }
func (p *pluginRuntimeGateway) Platform() string             { return p.platform }
func (p *pluginRuntimeGateway) Models() []sdk.ModelInfo      { return nil }
func (p *pluginRuntimeGateway) Routes() []sdk.RouteDefinition {
	return nil
}
func (p *pluginRuntimeGateway) Forward(ctx context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
	if p.forward == nil {
		return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}, nil
	}
	return p.forward(ctx, req)
}
func (p *pluginRuntimeGateway) ValidateAccount(context.Context, map[string]string) error {
	return nil
}
func (p *pluginRuntimeGateway) HandleWebSocket(context.Context, sdk.WebSocketConn) (sdk.ForwardOutcome, error) {
	return sdk.ForwardOutcome{}, sdk.ErrNotSupported
}

type pluginRuntimeExtension struct {
	id     string
	routes func(sdk.RouteRegistrar)
	tasks  []sdk.BackgroundTask
}

func (p *pluginRuntimeExtension) Info() sdk.PluginInfo {
	return sdk.PluginInfo{ID: p.id, Name: p.id, Type: sdk.PluginTypeExtension}
}
func (p *pluginRuntimeExtension) Init(sdk.PluginContext) error { return nil }
func (p *pluginRuntimeExtension) Start(context.Context) error  { return nil }
func (p *pluginRuntimeExtension) Stop(context.Context) error   { return nil }
func (p *pluginRuntimeExtension) RegisterRoutes(r sdk.RouteRegistrar) {
	if p.routes != nil {
		p.routes(r)
	}
}
func (p *pluginRuntimeExtension) Migrate() error { return nil }
func (p *pluginRuntimeExtension) BackgroundTasks() []sdk.BackgroundTask {
	return p.tasks
}

type pluginRuntimeMiddleware struct {
	id    string
	begin func(context.Context, *sdk.MiddlewareRequest) (*sdk.MiddlewareDecision, error)
	end   func(context.Context, *sdk.MiddlewareEvent) error
}

func (p *pluginRuntimeMiddleware) Info() sdk.PluginInfo {
	return sdk.PluginInfo{ID: p.id, Name: p.id, Type: sdk.PluginTypeMiddleware}
}
func (p *pluginRuntimeMiddleware) Init(sdk.PluginContext) error { return nil }
func (p *pluginRuntimeMiddleware) Start(context.Context) error  { return nil }
func (p *pluginRuntimeMiddleware) Stop(context.Context) error   { return nil }
func (p *pluginRuntimeMiddleware) OnForwardBegin(ctx context.Context, req *sdk.MiddlewareRequest) (*sdk.MiddlewareDecision, error) {
	if p.begin == nil {
		return nil, nil
	}
	return p.begin(ctx, req)
}
func (p *pluginRuntimeMiddleware) OnForwardEnd(ctx context.Context, evt *sdk.MiddlewareEvent) error {
	if p.end == nil {
		return nil
	}
	return p.end(ctx, evt)
}

func newGatewayRuntimeClient(t *testing.T, impl sdk.GatewayPlugin) (*sdkgrpc.GatewayGRPCClient, func()) {
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
	client, ok := rawClient.(*sdkgrpc.GatewayGRPCClient)
	if !ok {
		t.Fatalf("gateway client type = %T", rawClient)
	}
	return client, func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	}
}

func newExtensionRuntimeClient(t *testing.T, impl sdk.ExtensionPlugin) (*sdkgrpc.ExtensionGRPCClient, func()) {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	if err := (&sdkgrpc.ExtensionGRPCPlugin{Impl: impl}).GRPCServer(nil, server); err != nil {
		t.Fatalf("register extension gRPC server: %v", err)
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("extension gRPC server stopped with error: %v", err)
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
		t.Fatalf("dial extension gRPC server: %v", err)
	}

	rawClient, err := (&sdkgrpc.ExtensionGRPCPlugin{}).GRPCClient(ctx, nil, conn)
	if err != nil {
		t.Fatalf("create extension gRPC client: %v", err)
	}
	client, ok := rawClient.(*sdkgrpc.ExtensionGRPCClient)
	if !ok {
		t.Fatalf("extension client type = %T", rawClient)
	}
	return client, func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	}
}

func newMiddlewareRuntimeClient(t *testing.T, impl sdk.MiddlewarePlugin) (*sdkgrpc.MiddlewareGRPCClient, func()) {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	if err := (&sdkgrpc.MiddlewareGRPCPlugin{Impl: impl}).GRPCServer(nil, server); err != nil {
		t.Fatalf("register middleware gRPC server: %v", err)
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("middleware gRPC server stopped with error: %v", err)
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
		t.Fatalf("dial middleware gRPC server: %v", err)
	}

	rawClient, err := (&sdkgrpc.MiddlewareGRPCPlugin{}).GRPCClient(ctx, nil, conn)
	if err != nil {
		t.Fatalf("create middleware gRPC client: %v", err)
	}
	client, ok := rawClient.(*sdkgrpc.MiddlewareGRPCClient)
	if !ok {
		t.Fatalf("middleware client type = %T", rawClient)
	}
	return client, func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	}
}

func TestExtensionProxyRuntimeHTTPAndStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	client, cleanup := newExtensionRuntimeClient(t, &pluginRuntimeExtension{
		id: "demo",
		routes: func(r sdk.RouteRegistrar) {
			r.Handle(http.MethodPost, "/echo", func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("X-Allowed", "yes")
				w.Header().Set("X-Seen-Entry", req.Header.Get("X-Airgate-Entry"))
				w.Header().Set("X-Seen-Host", req.Header.Get("X-Forwarded-Host"))
				w.Header().Set("Set-Cookie", "blocked=1")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(req.Method + ":" + req.URL.RawQuery + ":" + string(body)))
			})
			r.Handle(http.MethodGet, "/stream", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("data: one\n\n"))
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				_, _ = w.Write([]byte("data: two\n\n"))
			})
			r.Handle(http.MethodGet, "/", func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("X-Seen-Entry", req.Header.Get("X-Airgate-Entry"))
				_, _ = w.Write([]byte("named"))
			})
		},
	})
	defer cleanup()

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["demo"] = &PluginInstance{Name: "demo", Extension: client}
	proxy := NewExtensionProxy(manager)
	router := gin.New()
	router.Any("/api/v1/ext/:pluginName/*path", proxy.Handle)
	router.Any("/api/v1/ext-user/:pluginName/*path", proxy.Handle)
	router.Any("/status", proxy.HandleNamed("demo", "public"))

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://core.example.test/api/v1/ext-user/demo/echo?debug=1", strings.NewReader("payload"))
	req.Host = "core.example.test"
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated || recorder.Body.String() != "POST:debug=1:payload" {
		t.Fatalf("echo response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Seen-Entry") != "user" || recorder.Header().Get("X-Seen-Host") != "core.example.test" {
		t.Fatalf("forwarded headers = %v", recorder.Header())
	}
	if recorder.Header().Get("X-Allowed") != "yes" || recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("response header filtering failed: %v", recorder.Header())
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ext/demo/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), "data: one") || !strings.Contains(recorder.Body.String(), "data: two") {
		t.Fatalf("stream response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Connection") != "" {
		t.Fatalf("blocked stream header leaked: %v", recorder.Header())
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "named" || recorder.Header().Get("X-Seen-Entry") != "public" {
		t.Fatalf("named response status=%d headers=%v body=%q", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestExtensionProxyRuntimeTransportErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	client, cleanup := newExtensionRuntimeClient(t, &pluginRuntimeExtension{
		id: "demo",
		routes: func(r sdk.RouteRegistrar) {
			r.Handle(http.MethodGet, "/ok", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("ok"))
			})
		},
	})
	cleanup()

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["demo"] = &PluginInstance{Name: "demo", Extension: client}
	proxy := NewExtensionProxy(manager)
	router := gin.New()
	router.Any("/api/v1/ext/:pluginName/*path", proxy.Handle)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ext/demo/ok", nil))
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "extension") {
		t.Fatalf("closed unary response status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ext/demo/ok", nil)
	req.Header.Set("Accept", "text/event-stream")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "extension") {
		t.Fatalf("closed stream response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestForwardMetadataOnlyThroughGatewayRuntime(t *testing.T) {
	var seenPath, seenMethod, seenQuery string
	client, cleanup := newGatewayRuntimeClient(t, &pluginRuntimeGateway{
		id:       "openai",
		platform: "openai",
		forward: func(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			seenPath = req.Headers.Get("X-Forwarded-Path")
			seenMethod = req.Headers.Get("X-Forwarded-Method")
			seenQuery = req.Headers.Get("X-Forwarded-Query")
			if req.Account == nil || req.Account.Platform != "openai" || req.Body == nil {
				t.Fatalf("metadata request = %+v", req)
			}
			return sdk.ForwardOutcome{
				Kind: sdk.OutcomeSuccess,
				Upstream: sdk.UpstreamResponse{
					StatusCode: http.StatusAccepted,
					Headers:    http.Header{"X-Plugin": {"metadata"}},
					Body:       []byte(`{"object":"list"}`),
				},
			}, nil
		},
	})
	defer cleanup()

	forwarder := &Forwarder{}
	c, recorder := pluginTestContext(http.MethodGet, "/v1/models?limit=1")
	forwarder.forwardMetadataOnly(c, &forwardState{
		requestPath:       "/v1/models",
		requestedPlatform: "openai",
		model:             "gpt-4.1",
		body:              []byte(`{}`),
		keyInfo:           &auth.APIKeyInfo{UserID: 1, KeyID: 2},
		plugin:            &PluginInstance{Name: "openai", Platform: "openai", Gateway: client},
	})
	if recorder.Code != http.StatusAccepted || recorder.Body.String() != `{"object":"list"}` {
		t.Fatalf("metadata response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Plugin") != "metadata" || seenPath != "/v1/models" || seenMethod != http.MethodGet || seenQuery != "limit=1" {
		t.Fatalf("metadata headers response=%v seen=%q/%q/%q", recorder.Header(), seenPath, seenMethod, seenQuery)
	}

	errorClient, errorCleanup := newGatewayRuntimeClient(t, &pluginRuntimeGateway{
		id:       "openai-error",
		platform: "openai",
		forward: func(context.Context, *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			return sdk.ForwardOutcome{}, errors.New("gateway down")
		},
	})
	defer errorCleanup()
	c, recorder = pluginTestContext(http.MethodGet, "/v1/models")
	forwarder.forwardMetadataOnly(c, &forwardState{
		requestPath:       "/v1/models",
		requestedPlatform: "openai",
		keyInfo:           &auth.APIKeyInfo{},
		plugin:            &PluginInstance{Name: "openai-error", Platform: "openai", Gateway: errorClient},
	})
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "metadata") {
		t.Fatalf("metadata error response status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	emptyClient, emptyCleanup := newGatewayRuntimeClient(t, &pluginRuntimeGateway{
		id:       "openai-empty",
		platform: "openai",
		forward: func(context.Context, *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Upstream: sdk.UpstreamResponse{StatusCode: http.StatusOK}}, nil
		},
	})
	defer emptyCleanup()
	c, recorder = pluginTestContext(http.MethodGet, "/v1/models")
	forwarder.forwardMetadataOnly(c, &forwardState{
		requestPath:       "/v1/models",
		requestedPlatform: "openai",
		keyInfo:           &auth.APIKeyInfo{},
		plugin:            &PluginInstance{Name: "openai-empty", Platform: "openai", Gateway: emptyClient},
	})
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "空响应") {
		t.Fatalf("metadata empty response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestQuotaAcquireClientAndAccountSlotsWithNilRedis(t *testing.T) {
	forwarder := &Forwarder{
		scheduler:   scheduler.NewScheduler(nil, nil),
		concurrency: scheduler.NewConcurrencyManager(nil),
	}

	c, _ := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	releaseClient := forwarder.acquireClientQuota(c, &forwardState{keyInfo: &auth.APIKeyInfo{
		UserID:             11,
		KeyID:              22,
		UserMaxConcurrency: 1,
		KeyMaxConcurrency:  1,
	}})
	if releaseClient == nil {
		t.Fatal("client quota with nil redis should fail open")
	}
	releaseClient()

	c, _ = pluginTestContext(http.MethodPost, "/v1/chat/completions")
	state := &forwardState{
		body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		account: &ent.Account{
			ID:    33,
			Extra: map[string]interface{}{"slot_ttl_seconds": 1},
		},
	}
	releaseAccount, ok := forwarder.acquireAccountSlot(c, state)
	if !ok || releaseAccount == nil || state.requestID == "" {
		t.Fatalf("account slot ok=%v release=%v requestID=%q", ok, releaseAccount != nil, state.requestID)
	}
	releaseAccount()

	c, _ = pluginTestContext(http.MethodPost, "/v1/chat/completions")
	state = &forwardState{
		body: []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		account: &ent.Account{
			ID: 44,
			Extra: map[string]interface{}{
				"msg_lock_enabled":      true,
				"msg_lock_ttl_seconds":  1,
				"msg_lock_wait_seconds": 1,
			},
		},
	}
	releaseAccount, ok = forwarder.acquireAccountSlot(c, state)
	if !ok || releaseAccount == nil {
		t.Fatalf("message-lock account slot ok=%v release=%v", ok, releaseAccount != nil)
	}
	releaseAccount()
}

func TestExtensionBackgroundTasksRuntime(t *testing.T) {
	ran := make(chan string, 2)
	client, cleanup := newExtensionRuntimeClient(t, &pluginRuntimeExtension{
		id: "demo",
		tasks: []sdk.BackgroundTask{
			{
				Name:     "sync",
				Interval: time.Millisecond,
				Handler: func(context.Context) error {
					ran <- "sync"
					return nil
				},
			},
			{
				Name:    "fail",
				Handler: func(context.Context) error { return errors.New("failed") },
			},
		},
	})
	defer cleanup()

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.startExtensionBackgroundTasks(nil)
	manager.startExtensionBackgroundTasks(&PluginInstance{Name: "no-extension"})

	emptyClient, emptyCleanup := newExtensionRuntimeClient(t, &pluginRuntimeExtension{id: "empty"})
	defer emptyCleanup()
	manager.startExtensionBackgroundTasks(&PluginInstance{Name: "empty", Extension: emptyClient})

	inst := &PluginInstance{Name: "demo", Extension: client}
	manager.startExtensionBackgroundTasks(inst)
	if inst.stopBackground == nil {
		t.Fatal("stopBackground was not installed")
	}
	select {
	case got := <-ran:
		if got != "sync" {
			t.Fatalf("background task = %q, want sync", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background task did not run")
	}
	inst.stopBackground()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	manager.runBackgroundTaskOnce(cancelled, "demo", client, "sync")
	manager.runBackgroundTaskOnce(context.Background(), "demo", client, "fail")
	manager.runBackgroundTaskLoop(cancelled, "demo", client, "sync", time.Hour)
}

func TestMiddlewareRuntimeBeginDenyErrorsAndEndOrder(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	appendOrder := func(value string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, value)
	}
	snapshotOrder := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), order...)
	}
	resetOrder := func() {
		mu.Lock()
		defer mu.Unlock()
		order = nil
	}

	first, cleanupFirst := newMiddlewareRuntimeClient(t, &pluginRuntimeMiddleware{
		id: "first",
		begin: func(_ context.Context, req *sdk.MiddlewareRequest) (*sdk.MiddlewareDecision, error) {
			appendOrder("first-begin")
			if req.UserID != 22 || req.AccountID != 44 || req.Model != "gpt-4.1" {
				t.Fatalf("middleware request = %+v", req)
			}
			return &sdk.MiddlewareDecision{Metadata: map[string]string{"first": "yes"}}, nil
		},
		end: func(_ context.Context, evt *sdk.MiddlewareEvent) error {
			appendOrder("first-end")
			if evt.Metadata["trace"] != "end" || evt.StatusCode != http.StatusAccepted {
				t.Fatalf("first end event = %+v", evt)
			}
			return nil
		},
	})
	defer cleanupFirst()
	second, cleanupSecond := newMiddlewareRuntimeClient(t, &pluginRuntimeMiddleware{
		id: "second",
		begin: func(_ context.Context, req *sdk.MiddlewareRequest) (*sdk.MiddlewareDecision, error) {
			appendOrder("second-begin")
			if req.Metadata["first"] != "yes" {
				t.Fatalf("metadata before second = %+v", req.Metadata)
			}
			return &sdk.MiddlewareDecision{
				Action:         sdk.DecisionDeny,
				DenyStatusCode: http.StatusTeapot,
				DenyMessage:    "blocked by middleware",
				Metadata:       map[string]string{"second": "yes"},
			}, nil
		},
		end: func(context.Context, *sdk.MiddlewareEvent) error {
			appendOrder("second-end")
			return nil
		},
	})
	defer cleanupSecond()

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["first"] = &PluginInstance{Name: "first", Priority: 10, Middleware: first}
	manager.instances["second"] = &PluginInstance{Name: "second", Priority: 20, Middleware: second}
	forwarder := &Forwarder{manager: manager}
	c, recorder := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	state := testForwardState()

	allowed, bag := forwarder.runForwardBeginChain(c, state)
	if allowed || recorder.Code != http.StatusTeapot || !strings.Contains(recorder.Body.String(), "blocked by middleware") {
		t.Fatalf("begin chain allowed=%v status=%d body=%q", allowed, recorder.Code, recorder.Body.String())
	}
	if bag["first"] != "yes" || bag["second"] != "yes" {
		t.Fatalf("metadata bag = %+v", bag)
	}
	if got := strings.Join(snapshotOrder(), ","); got != "first-begin,second-begin" {
		t.Fatalf("begin order = %s", got)
	}

	resetOrder()
	c, _ = pluginTestContext(http.MethodPost, "/v1/chat/completions")
	forwarder.runForwardEndChain(c, state, forwardExecution{
		duration: 10 * time.Millisecond,
		outcome:  sdk.ForwardOutcome{Upstream: sdk.UpstreamResponse{StatusCode: http.StatusAccepted}},
	}, map[string]string{"trace": "end"})
	if got := strings.Join(snapshotOrder(), ","); got != "second-end,first-end" {
		t.Fatalf("end order = %s", got)
	}

	resetOrder()
	failing, cleanupFailing := newMiddlewareRuntimeClient(t, &pluginRuntimeMiddleware{
		id: "failing",
		begin: func(context.Context, *sdk.MiddlewareRequest) (*sdk.MiddlewareDecision, error) {
			appendOrder("failing-begin")
			return nil, errors.New("boom")
		},
		end: func(context.Context, *sdk.MiddlewareEvent) error {
			appendOrder("failing-end")
			return errors.New("ignored")
		},
	})
	defer cleanupFailing()
	allowing, cleanupAllowing := newMiddlewareRuntimeClient(t, &pluginRuntimeMiddleware{
		id: "allowing",
		begin: func(context.Context, *sdk.MiddlewareRequest) (*sdk.MiddlewareDecision, error) {
			appendOrder("allowing-begin")
			return nil, nil
		},
	})
	defer cleanupAllowing()

	manager = NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["failing"] = &PluginInstance{Name: "failing", Priority: 1, Middleware: failing}
	manager.instances["allowing"] = &PluginInstance{Name: "allowing", Priority: 2, Middleware: allowing}
	forwarder = &Forwarder{manager: manager}
	c, _ = pluginTestContext(http.MethodPost, "/v1/chat/completions")
	allowed, bag = forwarder.runForwardBeginChain(c, state)
	if !allowed || len(bag) != 0 {
		t.Fatalf("error begin chain allowed=%v bag=%+v", allowed, bag)
	}
	if got := strings.Join(snapshotOrder(), ","); got != "failing-begin,allowing-begin" {
		t.Fatalf("error begin order = %s", got)
	}
}
