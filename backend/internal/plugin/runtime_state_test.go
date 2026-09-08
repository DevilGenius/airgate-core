package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	pb "github.com/DevilGenius/airgate-sdk/protocol/proto"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type stateHostConnection struct{ handle *pluginHostHandle }

func (c stateHostConnection) Invoke(ctx context.Context, req sdk.HostInvokeRequest) (*sdk.HostInvokeResponse, error) {
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return nil, err
	}
	r, err := c.handle.Invoke(ctx, &pb.HostInvokeRequest{Method: req.Method, Payload: payload})
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(r.Payload, &result); err != nil {
		return nil, err
	}
	return &sdk.HostInvokeResponse{Status: r.Status, Payload: result}, nil
}
func (c stateHostConnection) InvokeStream(context.Context, sdk.HostStreamRequest) (sdk.HostStream, error) {
	return nil, errors.New("unused")
}

func TestRuntimeStateTwoConnectionsCASAndIsolation(t *testing.T) {
	m := NewManager(t.TempDir(), "error", "", nil)
	defer m.devWatcher.Close()
	h := NewHostService(nil, m, nil, nil, nil, nil)
	connect := func(name string) *sdk.RuntimeStateClient {
		handle := h.NewPluginHandle(name)
		handle.SetCapabilities(map[sdk.Capability]bool{sdk.CapabilityForHostMethod(sdk.RuntimeStateMethod): true})
		return &sdk.RuntimeStateClient{Host: stateHostConnection{handle}}
	}
	a, b, foreign := connect("same"), connect("same"), connect("foreign")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, client := range []*sdk.RuntimeStateClient{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, err := client.Update(ctx, "counter", func(value string) (string, error) { n, _ := strconv.Atoi(value); return strconv.Itoa(n + 1), nil })
				if err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	value, version, found, err := a.Get(ctx, "counter")
	if err != nil || !found || value != "200" {
		t.Fatalf("lost update: %s %t %v", value, found, err)
	}
	if ok, err := b.CompareAndSwap(ctx, "counter", "0", "stale"); err != nil || ok {
		t.Fatalf("stale CAS accepted: %t %v", ok, err)
	}
	if _, _, found, err := foreign.Get(ctx, "counter"); err != nil || found {
		t.Fatalf("cross-plugin read: %t %v", found, err)
	}
	if ok, err := a.CompareAndSwap(ctx, "counter", version, "last"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	values, err := b.GetMany(ctx, []string{"counter", "missing"})
	if err != nil || values["counter"] != "last" || len(values) != 1 {
		t.Fatal(values, err)
	}
}

func TestRuntimeStateExpiryPreventsABA(t *testing.T) {
	var s runtimeStateStore
	call := func(action, version, value string) map[string]interface{} {
		raw, _ := json.Marshal(map[string]string{"action": action, "key": "k", "version": version, "value": value})
		r, err := s.invoke(context.Background(), "p", raw)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := call("cas", "0", "first")["version"].(string)
	s.mu.Lock()
	s.items[runtimeStateKey{"p", "k"}].Value.(*runtimeStateEntry).expiry = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if call("get", "", "")["found"] != false {
		t.Fatal("expired state remained")
	}
	second := call("cas", "0", "second")["version"].(string)
	if second == first || call("cas", first, "stale")["swapped"] != false {
		t.Fatal("expired version reused")
	}
}
