package proxy

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func TestListNormalizesPagination(t *testing.T) {
	var captured ListFilter
	service := NewService(proxyStubRepository{
		list: func(_ context.Context, filter ListFilter) ([]Proxy, int64, error) {
			captured = filter
			return nil, 0, nil
		},
	})

	result, err := service.List(t.Context(), ListFilter{})
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if captured.Page != 1 || captured.PageSize != 20 {
		t.Fatalf("List() normalized filter = %+v, want page=1 pageSize=20", captured)
	}
	if result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("List() result pagination = %+v, want page=1 pageSize=20", result)
	}
}

func TestTestReturnsNotFoundError(t *testing.T) {
	service := NewService(proxyStubRepository{
		findByID: func(_ context.Context, _ int) (Proxy, error) {
			return Proxy{}, ErrProxyNotFound
		},
	})

	_, err := service.Test(t.Context(), 7)
	if !errors.Is(err, ErrProxyNotFound) {
		t.Fatalf("Test() error = %v, want ErrProxyNotFound", err)
	}
}

func TestTestUsesConfiguredProber(t *testing.T) {
	service := NewService(proxyStubRepository{
		findByID: func(_ context.Context, id int) (Proxy, error) {
			return Proxy{ID: id, Name: "p1"}, nil
		},
	})
	service.prober = stubProber{
		probe: func(_ context.Context, item Proxy) TestResult {
			if item.ID != 9 {
				t.Fatalf("prober got proxy id=%d, want 9", item.ID)
			}
			return TestResult{Success: true, Latency: 12}
		},
	}

	result, err := service.Test(t.Context(), 9)
	if err != nil {
		t.Fatalf("Test() returned error: %v", err)
	}
	if !result.Success || result.Latency != 12 {
		t.Fatalf("Test() result = %+v, want success latency=12", result)
	}
}

func TestLookupIPUsesConfiguredProber(t *testing.T) {
	service := NewService(proxyStubRepository{
		findByID: func(_ context.Context, id int) (Proxy, error) {
			return Proxy{ID: id, Name: "p1"}, nil
		},
	})
	service.prober = stubProber{
		lookupIP: func(_ context.Context, item Proxy) TestResult {
			if item.ID != 9 {
				t.Fatalf("prober got proxy id=%d, want 9", item.ID)
			}
			return TestResult{Success: true, IPAddress: "1.2.3.4"}
		},
	}

	result, err := service.LookupIP(t.Context(), 9)
	if err != nil {
		t.Fatalf("LookupIP() returned error: %v", err)
	}
	if !result.Success || result.IPAddress != "1.2.3.4" {
		t.Fatalf("LookupIP() result = %+v", result)
	}
}

func TestCreateUpdateDeleteDelegateToRepository(t *testing.T) {
	service := NewService(proxyStubRepository{
		create: func(_ context.Context, input CreateInput) (Proxy, error) {
			if input.Name != "p1" {
				t.Fatalf("创建输入异常: %+v", input)
			}
			return Proxy{ID: 1, Name: input.Name}, nil
		},
		update: func(_ context.Context, id int, input UpdateInput) (Proxy, error) {
			if id != 1 || input.Name == nil || *input.Name != "p2" {
				t.Fatalf("更新输入异常: id=%d input=%+v", id, input)
			}
			return Proxy{ID: id, Name: *input.Name}, nil
		},
		delete: func(_ context.Context, id int) error {
			if id != 1 {
				t.Fatalf("删除 ID = %d，期望 1", id)
			}
			return nil
		},
	})

	created, err := service.Create(t.Context(), CreateInput{Name: "p1"})
	if err != nil || created.ID != 1 {
		t.Fatalf("创建结果异常: %+v, %v", created, err)
	}
	name := "p2"
	updated, err := service.Update(t.Context(), 1, UpdateInput{Name: &name})
	if err != nil || updated.Name != "p2" {
		t.Fatalf("更新结果异常: %+v, %v", updated, err)
	}
	if err := service.Delete(t.Context(), 1); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
}

func TestRepositoryErrorsPropagate(t *testing.T) {
	repoErr := errors.New("repo failed")
	service := NewService(proxyStubRepository{
		list:     func(context.Context, ListFilter) ([]Proxy, int64, error) { return nil, 0, repoErr },
		create:   func(context.Context, CreateInput) (Proxy, error) { return Proxy{}, repoErr },
		update:   func(context.Context, int, UpdateInput) (Proxy, error) { return Proxy{}, repoErr },
		delete:   func(context.Context, int) error { return repoErr },
		findByID: func(context.Context, int) (Proxy, error) { return Proxy{}, repoErr },
	})

	if _, err := service.List(t.Context(), ListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("List error = %v", err)
	}
	if _, err := service.Create(t.Context(), CreateInput{Name: "p1"}); !errors.Is(err, repoErr) {
		t.Fatalf("Create error = %v", err)
	}
	if _, err := service.Update(t.Context(), 1, UpdateInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("Update error = %v", err)
	}
	if err := service.Delete(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("Delete error = %v", err)
	}
	if _, err := service.Test(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("Test error = %v", err)
	}
	if _, err := service.LookupIP(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("LookupIP error = %v", err)
	}
}

func TestTestLogsFailedProbe(t *testing.T) {
	service := NewService(proxyStubRepository{
		findByID: func(_ context.Context, id int) (Proxy, error) {
			return Proxy{ID: id, Protocol: "http", Address: "127.0.0.1"}, nil
		},
	})
	service.prober = stubProber{
		probe: func(context.Context, Proxy) TestResult {
			return TestResult{Success: false, ErrorMsg: "dial failed"}
		},
	}

	result, err := service.Test(t.Context(), 3)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if result.Success || result.ErrorMsg != "dial failed" {
		t.Fatalf("Test() result = %+v", result)
	}
}

func TestBuildProxyTransportForHTTPProxyWithAuth(t *testing.T) {
	transport, err := buildProxyTransport(Proxy{
		Protocol: "http",
		Address:  "127.0.0.1",
		Port:     8080,
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("构建 HTTP 代理失败: %v", err)
	}
	if transport.Proxy == nil {
		t.Fatal("HTTP 代理应设置 Proxy 函数")
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("获取代理 URL 失败: %v", err)
	}
	if proxyURL.String() != "http://user:pass@127.0.0.1:8080" {
		t.Fatalf("代理 URL = %q，期望带认证信息", proxyURL.String())
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if got := transport.ProxyConnectHeader.Get("Proxy-Authorization"); got != wantAuth {
		t.Fatalf("代理认证头 = %q，期望 %q", got, wantAuth)
	}
}

func TestBuildProxyTransportForHTTPProxyWithoutAuth(t *testing.T) {
	transport, err := buildProxyTransport(Proxy{
		Protocol: "http",
		Address:  "127.0.0.1",
		Port:     8080,
	})
	if err != nil {
		t.Fatalf("构建 HTTP 代理失败: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("获取代理 URL 失败: %v", err)
	}
	if proxyURL.String() != "http://127.0.0.1:8080" {
		t.Fatalf("代理 URL = %q，期望无认证信息", proxyURL.String())
	}
	if got := transport.ProxyConnectHeader.Get("Proxy-Authorization"); got != "" {
		t.Fatalf("代理认证头 = %q，期望空", got)
	}
}

func TestBuildProxyTransportForSOCKS5(t *testing.T) {
	for _, item := range []Proxy{
		{Protocol: "socks5", Address: "127.0.0.1", Port: 1080},
		{Protocol: "socks5", Address: "127.0.0.1", Port: 1080, Username: "user", Password: "pass"},
	} {
		transport, err := buildProxyTransport(item)
		if err != nil {
			t.Fatalf("构建 SOCKS5 代理失败: %v", err)
		}
		if transport.DialContext == nil {
			t.Fatal("SOCKS5 transport should set DialContext")
		}
	}
}

func TestBuildProxyTransportRejectsUnsupportedProtocol(t *testing.T) {
	_, err := buildProxyTransport(Proxy{Protocol: "ftp", Address: "127.0.0.1", Port: 21})
	if err == nil {
		t.Fatal("不支持的代理协议应返回错误")
	}
}

func TestDefaultProberReturnsBuildError(t *testing.T) {
	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "ftp", Address: "127.0.0.1", Port: 21})
	if result.Success || result.ErrorMsg == "" {
		t.Fatalf("Probe() = %+v, want build error", result)
	}
}

func TestDefaultProberUsesPrimaryIPEndpoint(t *testing.T) {
	restore := replaceProbeHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Host != "ip-api.com" {
			t.Fatalf("request = %s %s", req.Method, req.URL.String())
		}
		return stringResponse(http.StatusOK, `{"status":"success","query":"1.2.3.4","country":"美国","countryCode":"US","city":"纽约"}`), nil
	})
	defer restore()

	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
	if !result.Success || result.IPAddress != "1.2.3.4" || result.CountryCode != "US" || result.City != "纽约" {
		t.Fatalf("Probe() = %+v", result)
	}
}

func TestDefaultProberFallsBackToHTTPBinEndpoint(t *testing.T) {
	restore := replaceProbeHTTPClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "ip-api.com":
			return stringResponse(http.StatusOK, `{"status":"fail"}`), nil
		case "httpbin.org":
			return stringResponse(http.StatusOK, `{"origin":"5.6.7.8"}`), nil
		default:
			t.Fatalf("unexpected host %q", req.URL.Host)
			return nil, nil
		}
	})
	defer restore()

	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
	if !result.Success || result.IPAddress != "5.6.7.8" {
		t.Fatalf("Probe() = %+v", result)
	}
}

func TestDefaultProberFallsBackToProviderHeadProbe(t *testing.T) {
	restore := replaceProbeHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return stringResponse(http.StatusBadGateway, "bad gateway"), nil
		}
		if req.Method == http.MethodHead && req.URL.Host == "api.openai.com" {
			return stringResponse(http.StatusNoContent, ""), nil
		}
		return nil, io.EOF
	})
	defer restore()

	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
	if !result.Success || result.IPAddress != "" {
		t.Fatalf("Probe() = %+v", result)
	}
}

func TestDefaultProberHandlesHTTPBinParseFailureBeforeHeadFallback(t *testing.T) {
	restore := replaceProbeHTTPClient(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Host == "ip-api.com":
			return stringResponse(http.StatusOK, `{"status":"fail"}`), nil
		case req.Method == http.MethodGet && req.URL.Host == "httpbin.org":
			return stringResponse(http.StatusOK, `{`), nil
		case req.Method == http.MethodHead && req.URL.Host == "api.openai.com":
			return stringResponse(http.StatusNoContent, ""), nil
		default:
			return nil, io.EOF
		}
	})
	defer restore()

	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
	if !result.Success || result.IPAddress != "" {
		t.Fatalf("Probe() = %+v", result)
	}
}

func TestDefaultProberFallsBackToHTTPConnectivityTarget(t *testing.T) {
	restore := replaceProbeHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodHead && req.URL.Host == "cp.cloudflare.com" {
			return stringResponse(http.StatusNoContent, ""), nil
		}
		return nil, io.EOF
	})
	defer restore()

	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
	if !result.Success || result.IPAddress != "" {
		t.Fatalf("Probe() = %+v, want connectivity-only success", result)
	}
}

func TestDefaultProberUsesIPEndpointResponseAsConnectivityFallback(t *testing.T) {
	restore := replaceProbeHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Host == "ip-api.com" {
			return stringResponse(http.StatusForbidden, "access denied"), nil
		}
		return nil, io.EOF
	})
	defer restore()

	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
	if !result.Success || result.IPAddress != "" {
		t.Fatalf("Probe() = %+v, want connectivity-only success", result)
	}
}

func TestDefaultProberDoesNotTreatProxyAuthFailureAsConnectivity(t *testing.T) {
	oldEndpoints := proxyProbeEndpoints
	oldFallbackTargets := proxyProbeFallbackTargets
	proxyProbeEndpoints = []probeEndpoint{{
		url: "http://probe.local/ip",
		parse: func([]byte) (string, string, string, string) {
			return "", "", "", ""
		},
	}}
	proxyProbeFallbackTargets = []string{"http://fallback.local"}
	defer func() {
		proxyProbeEndpoints = oldEndpoints
		proxyProbeFallbackTargets = oldFallbackTargets
	}()

	restore := replaceProbeHTTPClient(func(*http.Request) (*http.Response, error) {
		return stringResponse(http.StatusProxyAuthRequired, "proxy authentication required"), nil
	})
	defer restore()

	result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
	if result.Success || !strings.Contains(result.ErrorMsg, "HTTP 407") {
		t.Fatalf("Probe() = %+v, want proxy authentication failure", result)
	}
}

func TestDefaultProberReportsRequestCreationAndDoErrors(t *testing.T) {
	t.Run("bad urls", func(t *testing.T) {
		oldEndpoints := proxyProbeEndpoints
		oldFallbackTargets := proxyProbeFallbackTargets
		proxyProbeEndpoints = []probeEndpoint{{
			url: "://bad-url",
			parse: func([]byte) (string, string, string, string) {
				return "", "", "", ""
			},
		}}
		proxyProbeFallbackTargets = []string{"://bad-head"}
		defer func() {
			proxyProbeEndpoints = oldEndpoints
			proxyProbeFallbackTargets = oldFallbackTargets
		}()

		result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
		if result.Success || !strings.Contains(result.ErrorMsg, "创建请求失败") {
			t.Fatalf("Probe() = %+v, want request creation error", result)
		}
	})

	t.Run("transport errors", func(t *testing.T) {
		oldEndpoints := proxyProbeEndpoints
		oldFallbackTargets := proxyProbeFallbackTargets
		proxyProbeEndpoints = []probeEndpoint{{
			url: "http://probe.local/ip",
			parse: func([]byte) (string, string, string, string) {
				return "", "", "", ""
			},
		}}
		proxyProbeFallbackTargets = []string{"https://fallback.local"}
		defer func() {
			proxyProbeEndpoints = oldEndpoints
			proxyProbeFallbackTargets = oldFallbackTargets
		}()
		restore := replaceProbeHTTPClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial failed")
		})
		defer restore()

		result := DefaultProber{}.Probe(t.Context(), Proxy{Protocol: "http", Address: "127.0.0.1", Port: 8080})
		if result.Success || !strings.Contains(result.ErrorMsg, "请求失败") {
			t.Fatalf("Probe() = %+v, want transport error", result)
		}
	})
}

func TestBuildProxyTransportReturnsSOCKS5FactoryError(t *testing.T) {
	oldSOCKS5 := newSOCKS5Dialer
	newSOCKS5Dialer = func(string, string, *xproxy.Auth, xproxy.Dialer) (xproxy.Dialer, error) {
		return nil, errors.New("socks failed")
	}
	defer func() { newSOCKS5Dialer = oldSOCKS5 }()

	if _, err := buildProxyTransport(Proxy{Protocol: "socks5", Address: "127.0.0.1", Port: 1080}); err == nil || !strings.Contains(err.Error(), "SOCKS5") {
		t.Fatalf("buildProxyTransport socks error = %v", err)
	}
}

func TestBuildProxyTransportSOCKS5DialContextUsesDialer(t *testing.T) {
	oldSOCKS5 := newSOCKS5Dialer
	newSOCKS5Dialer = func(string, string, *xproxy.Auth, xproxy.Dialer) (xproxy.Dialer, error) {
		return dialerFunc(func(network, address string) (net.Conn, error) {
			if network != "tcp" || address != "example.com:443" {
				t.Fatalf("dial = %s %s", network, address)
			}
			return nil, errors.New("dial invoked")
		}), nil
	}
	defer func() { newSOCKS5Dialer = oldSOCKS5 }()

	transport, err := buildProxyTransport(Proxy{Protocol: "socks5", Address: "127.0.0.1", Port: 1080})
	if err != nil {
		t.Fatalf("buildProxyTransport socks5: %v", err)
	}
	if _, err := transport.DialContext(t.Context(), "tcp", "example.com:443"); err == nil || !strings.Contains(err.Error(), "dial invoked") {
		t.Fatalf("DialContext error = %v", err)
	}
}

type stubProber struct {
	probe             func(context.Context, Proxy) TestResult
	probeConnectivity func(context.Context, Proxy) TestResult
	lookupIP          func(context.Context, Proxy) TestResult
}

func (s stubProber) Probe(ctx context.Context, p Proxy) TestResult {
	return s.probe(ctx, p)
}

func (s stubProber) ProbeConnectivity(ctx context.Context, p Proxy) TestResult {
	if s.probeConnectivity != nil {
		return s.probeConnectivity(ctx, p)
	}
	return s.probe(ctx, p)
}

func (s stubProber) LookupIP(ctx context.Context, p Proxy) TestResult {
	if s.lookupIP != nil {
		return s.lookupIP(ctx, p)
	}
	return s.probe(ctx, p)
}

type proxyStubRepository struct {
	list     func(context.Context, ListFilter) ([]Proxy, int64, error)
	findByID func(context.Context, int) (Proxy, error)
	create   func(context.Context, CreateInput) (Proxy, error)
	update   func(context.Context, int, UpdateInput) (Proxy, error)
	delete   func(context.Context, int) error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func replaceProbeHTTPClient(fn func(*http.Request) (*http.Response, error)) func() {
	oldClient := newProxyProbeClient
	newProxyProbeClient = func(http.RoundTripper, time.Duration) *http.Client {
		return &http.Client{Transport: roundTripFunc(fn)}
	}
	return func() {
		newProxyProbeClient = oldClient
	}
}

func stringResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

type dialerFunc func(network, address string) (net.Conn, error)

func (f dialerFunc) Dial(network, address string) (net.Conn, error) {
	return f(network, address)
}

func (s proxyStubRepository) List(ctx context.Context, filter ListFilter) ([]Proxy, int64, error) {
	if s.list == nil {
		return nil, 0, nil
	}
	return s.list(ctx, filter)
}

func (s proxyStubRepository) FindByID(ctx context.Context, id int) (Proxy, error) {
	if s.findByID == nil {
		return Proxy{}, nil
	}
	return s.findByID(ctx, id)
}

func (s proxyStubRepository) Create(ctx context.Context, input CreateInput) (Proxy, error) {
	if s.create == nil {
		return Proxy{}, nil
	}
	return s.create(ctx, input)
}

func (s proxyStubRepository) Update(ctx context.Context, id int, input UpdateInput) (Proxy, error) {
	if s.update == nil {
		return Proxy{}, nil
	}
	return s.update(ctx, id, input)
}

func (s proxyStubRepository) Delete(ctx context.Context, id int) error {
	if s.delete == nil {
		return nil
	}
	return s.delete(ctx, id)
}
