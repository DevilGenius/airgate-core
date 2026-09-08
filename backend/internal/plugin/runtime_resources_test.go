package plugin

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type callbackRuntimeFixture struct {
	pluginRuntimeGateway
	value string
}

func (g *callbackRuntimeFixture) HandleRuntimeCallback(_ context.Context, r sdk.CallbackRequest) (sdk.CallbackResponse, error) {
	return sdk.CallbackResponse{Status: 200, Body: []byte(g.value + ":" + r.Name)}, nil
}

func TestRuntimeCallbackResourcesTransferWithoutProviderKnowledge(t *testing.T) {
	m := NewManager(t.TempDir(), "error", "", nil)
	defer m.StopAll(context.Background())
	makeGeneration := func(id, value string) *PluginInstance {
		client, cleanup := newGatewayRuntimeClient(t, &callbackRuntimeFixture{pluginRuntimeGateway: pluginRuntimeGateway{id: "arbitrary", platform: "custom"}, value: value})
		t.Cleanup(cleanup)
		return &PluginInstance{Name: "arbitrary", Generation: id, Gateway: client, runtime: client, owner: m}
	}
	spec := sdk.RuntimeSpec{Version: 1, Callbacks: []sdk.CallbackSpec{{Name: "example", Listen: "127.0.0.1:0", Required: true}}}
	a, b := makeGeneration("old", "old"), makeGeneration("new", "new")
	first, err := m.prepareRuntimeResources(t.Context(), a, spec)
	if err != nil {
		t.Fatal(err)
	}
	m.instances[a.Name] = a
	url := "http://" + first.Callbacks["example"].Address + "/anything?opaque=1"
	get := func(expected string) {
		t.Helper()
		r, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 || string(data) != expected {
			t.Fatalf("%d %q", r.StatusCode, data)
		}
	}
	get("old:example")
	second, err := m.prepareRuntimeResources(t.Context(), b, spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.Callbacks["example"].Address != second.Callbacks["example"].Address {
		t.Fatal("listener rebound at replacement")
	}
	m.mu.Lock()
	a.beginDrain()
	m.instances[b.Name] = b
	m.mu.Unlock()
	m.releaseRuntimeResources(a)
	get("new:example")
	failed := makeGeneration("failed", "failed")
	bad := sdk.RuntimeSpec{Callbacks: append(append([]sdk.CallbackSpec{}, spec.Callbacks...), sdk.CallbackSpec{Name: "invalid", Listen: "0.0.0.0:12345"})}
	if _, err := m.prepareRuntimeResources(t.Context(), failed, bad); err == nil {
		t.Fatal("non-loopback resource accepted")
	}
	m.releaseRuntimeResources(failed)
	get("new:example")
}

func TestGenerationRetirementHasThirtyMinutePolicy(t *testing.T) {
	if pluginRetirementTimeout != 30*time.Minute {
		t.Fatal("unexpected retirement policy", pluginRetirementTimeout)
	}
	m, _ := generationTestManager(t)
	old := m.GetInstance("generation-test")
	if !old.acquireRequest() {
		t.Fatal("cannot pin old process")
	}
	m.mu.Lock()
	idle := old.beginDrain()
	delete(m.instances, old.Name)
	m.retiring[old.Name] = old
	m.mu.Unlock()
	started := time.Now()
	m.retireGenerationWithin(old, idle, 30*time.Millisecond)
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond || elapsed > time.Second {
		t.Fatal("retirement did not respect deadline", elapsed)
	}
	select {
	case <-old.stopped:
	default:
		t.Fatal("expired process not stopped")
	}
	if !old.Client.Exited() {
		t.Fatal("expired process still alive")
	}
	old.releaseRequest()
}
