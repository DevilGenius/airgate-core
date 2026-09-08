package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// The test executable is a real go-plugin process when spawned as an artifact.
// No live provider accounts or externally billed requests are used.
func runGenerationFixture() bool {
	if os.Getenv("AIRGATE_GENERATION_TEST_PROCESS") != "1" {
		return false
	}
	sdkgrpc.Serve(&generationFixture{pluginRuntimeGateway: pluginRuntimeGateway{id: "generation-test", platform: "fixture"}, version: os.Getenv("AIRGATE_GENERATION_TEST_VERSION")})
	return true
}

type generationFixture struct {
	pluginRuntimeGateway
	version string
	host    sdk.Host
}

func (g *generationFixture) Info() sdk.PluginInfo {
	return sdk.PluginInfo{ID: g.id, Name: g.id, Version: g.version, Type: sdk.PluginTypeGateway, Capabilities: []sdk.Capability{sdk.CapabilityForHostMethod(sdk.RuntimeStateMethod)}}
}
func (g *generationFixture) Init(ctx sdk.PluginContext) error {
	g.host = ctx.(sdk.HostAware).Host()
	return nil
}
func (g *generationFixture) HandleRequest(ctx context.Context, method, path, query string, headers http.Header, body []byte) (int, http.Header, []byte, error) {
	state := &sdk.RuntimeStateClient{Host: g.host}
	if method == http.MethodPut {
		_, err := state.Update(ctx, "session", func(string) (string, error) { return string(body), nil })
		return 200, nil, nil, err
	}
	value, _, _, err := state.Get(ctx, "session")
	return 200, nil, []byte(value), err
}
func (g *generationFixture) Start(ctx context.Context) error {
	if g.version == "fail" {
		return errors.New("candidate start failed")
	}
	if path := os.Getenv("AIRGATE_GENERATION_TEST_BLOCK_START"); path != "" {
		_ = os.WriteFile(path, []byte("started"), 0600)
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (g *generationFixture) GetWebAssets() map[string][]byte {
	return map[string][]byte{"index.js": []byte(g.version)}
}
func (g *generationFixture) Forward(ctx context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
	if _, err := fmt.Fprint(req.Writer, g.version+":start\n"); err != nil {
		return sdk.ForwardOutcome{}, err
	}
	if f, ok := req.Writer.(http.Flusher); ok {
		f.Flush()
	}
	if len(req.Body) > 0 {
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := os.Stat(string(req.Body)); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return sdk.ForwardOutcome{}, ctx.Err()
			case <-tick.C:
			}
		}
	}
	_, err := fmt.Fprint(req.Writer, g.version+":end\n")
	return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Upstream: sdk.UpstreamResponse{StatusCode: 200}}, err
}

type generationWriter struct {
	*httptest.ResponseRecorder
	started chan struct{}
	once    sync.Once
}

func (w *generationWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	w.once.Do(func() { close(w.started) })
	return n, err
}

func generationTestManager(t *testing.T) (*Manager, []byte) {
	t.Helper()
	t.Setenv("AIRGATE_GENERATION_TEST_PROCESS", "1")
	t.Setenv("AIRGATE_GENERATION_TEST_VERSION", "v1")
	t.Setenv("AIRGATE_GENERATION_TEST_BLOCK_START", "")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(t.TempDir(), "error", "", nil)
	m.SetHostService(NewHostService(nil, m, nil, nil, nil, nil))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		m.StopAll(ctx)
	})
	if err := m.InstallFromBinary(context.Background(), "generation-test", binary); err != nil {
		t.Fatal(err)
	}
	return m, binary
}

func assertGenerationReply(t *testing.T, inst *PluginInstance, version string) {
	t.Helper()
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := inst.Forward(ctx, &sdk.ForwardRequest{Account: &sdk.Account{ID: 1}, Stream: true, Writer: w})
	if err != nil || w.Body.String() != version+":start\n"+version+":end\n" {
		t.Fatalf("reply %q error %v", w.Body.String(), err)
	}
}

func TestGenerationProcessCutoverAndFailureRecovery(t *testing.T) {
	m, binary := generationTestManager(t)
	old := m.GetInstance("generation-test")
	gate := filepath.Join(t.TempDir(), "release")
	w := &generationWriter{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{})}
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() {
		_, err := old.Forward(ctx, &sdk.ForwardRequest{Account: &sdk.Account{ID: 1}, Stream: true, Writer: w, Body: []byte(gate)})
		done <- err
	}()
	select {
	case <-w.started:
	case <-time.After(3 * time.Second):
		t.Fatal("old stream did not start")
	}
	t.Setenv("AIRGATE_GENERATION_TEST_VERSION", "v2")
	if err := m.InstallFromBinary(ctx, "generation-test", binary); err != nil {
		t.Fatal(err)
	}
	current := m.GetInstance("generation-test")
	if current == old || current.Version != "v2" || old.activeRequestCount() != 1 {
		t.Fatal("generation did not switch with live old stream")
	}
	assertGenerationReply(t, old, "v2") // Reference fetched before cutover is safe.
	if _, _, _, err := old.Gateway.HandleHTTPRequest(ctx, http.MethodPut, "state", "", nil, []byte("late old response")); err != nil {
		t.Fatal(err)
	}
	if _, _, value, err := current.HandleHTTPRequest(ctx, http.MethodGet, "state", "", nil, nil); err != nil || string(value) != "late old response" {
		t.Fatalf("new process read stale session: %q %v", value, err)
	}
	select {
	case err := <-done:
		t.Fatalf("update interrupted old stream: %v", err)
	default:
	}
	if err := m.InstallFromBinary(ctx, "generation-test", binary); !errors.Is(err, ErrPluginUpdateBusy) {
		t.Fatalf("third process admitted: %v", err)
	}
	if err := os.WriteFile(gate, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != "v1:start\nv1:end\n" {
		t.Fatalf("old stream mixed generations: %q", w.Body.String())
	}
	select {
	case <-old.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("old process not reaped")
	}
	waitNoRetire(t, m, "generation-test")
	t.Setenv("AIRGATE_GENERATION_TEST_VERSION", "fail")
	if err := m.InstallFromBinary(ctx, "generation-test", binary); err == nil {
		t.Fatal("failed candidate activated")
	}
	if m.GetInstance("generation-test") != current {
		t.Fatal("failed update replaced active instance")
	}
	manifest, err := m.loadArtifact("generation-test")
	if err != nil || manifest.Generation != current.Generation {
		t.Fatalf("failed update changed active manifest: %+v %v", manifest, err)
	}
	assertGenerationReply(t, current, "v2")
	assets, err := m.ReadPluginAsset("generation-test", "index.js")
	if err != nil || string(assets) != "v2" {
		t.Fatalf("assets %q %v", assets, err)
	}
	m.StopAll(ctx)
	t.Setenv("AIRGATE_GENERATION_TEST_VERSION", "v2")
	restarted := NewManager(m.pluginDir, "error", "", nil)
	restarted.SetHostService(NewHostService(nil, restarted, nil, nil, nil, nil))
	defer restarted.StopAll(ctx)
	if err := restarted.LoadAll(ctx); err != nil {
		t.Fatal(err)
	}
	assertGenerationReply(t, restarted.GetInstance("generation-test"), "v2")
}

func waitNoRetire(t *testing.T, m *Manager, name string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		pending := m.retiring[name] != nil
		m.mu.RUnlock()
		if !pending {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("retirement remained registered")
}

func TestGenerationShutdownCancelsCandidate(t *testing.T) {
	m, binary := generationTestManager(t)
	signal := filepath.Join(t.TempDir(), "preparing")
	t.Setenv("AIRGATE_GENERATION_TEST_BLOCK_START", signal)
	done := make(chan error, 1)
	go func() { done <- m.InstallFromBinary(context.Background(), "generation-test", binary) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(signal); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("candidate did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.StopAll(ctx)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("shutdown published candidate")
		}
	case <-time.After(time.Second):
		t.Fatal("candidate remained running")
	}
	if err := m.InstallFromBinary(context.Background(), "generation-test", binary); err == nil || !strings.Contains(err.Error(), "stopping") {
		t.Fatalf("manager reopened after shutdown: %v", err)
	}
}
