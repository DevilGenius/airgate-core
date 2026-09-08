package plugin

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type runtimeCallbackKey struct{ plugin, name, listen string }
type runtimeCallbackResource struct {
	key     runtimeCallbackKey
	server  *http.Server
	address string
	claims  map[string]bool
	closed  chan struct{}
	slots   chan struct{}
}

// Resources are declared through SDK RuntimeSpec. Core knows nothing about
// OAuth, providers, state formats, callback query fields or callback bodies.
func (m *Manager) prepareRuntimeResources(ctx context.Context, inst *PluginInstance, spec sdk.RuntimeSpec) (sdk.RuntimeContext, error) {
	info := sdk.RuntimeContext{Generation: inst.Generation, Callbacks: make(map[string]sdk.CallbackBinding)}
	if len(spec.Callbacks) > 4 {
		return info, fmt.Errorf("too many callback resources")
	}
	for _, declaration := range spec.Callbacks {
		if _, duplicate := info.Callbacks[declaration.Name]; duplicate || !pluginIDPattern.MatchString(declaration.Name) {
			return info, fmt.Errorf("invalid callback name")
		}
		host, portText, err := net.SplitHostPort(declaration.Listen)
		if err != nil {
			return info, fmt.Errorf("invalid callback address")
		}
		ip := net.ParseIP(host)
		port, err := strconv.Atoi(portText)
		if ip == nil || !ip.IsLoopback() || err != nil || port < 0 || port > 65535 || (port > 0 && port < 1024) {
			return info, fmt.Errorf("callbacks require an unprivileged loopback address")
		}
		key := runtimeCallbackKey{inst.Name, declaration.Name, net.JoinHostPort(ip.String(), strconv.Itoa(port))}
		resource, err := m.claimRuntimeCallback(ctx, inst.Generation, key)
		if err != nil {
			if declaration.Required {
				return info, err
			}
			info.Callbacks[declaration.Name] = sdk.CallbackBinding{Error: err.Error()}
			continue
		}
		inst.callbacks = append(inst.callbacks, resource)
		info.Callbacks[declaration.Name] = sdk.CallbackBinding{Available: true, Address: resource.address}
	}
	return info, nil
}
func (m *Manager) claimRuntimeCallback(ctx context.Context, generation string, key runtimeCallbackKey) (*runtimeCallbackResource, error) {
	m.callbackMu.Lock()
	defer m.callbackMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.runtimeCtx.Err() != nil {
		return nil, fmt.Errorf("core is stopping")
	}
	if m.callbacks == nil {
		m.callbacks = make(map[runtimeCallbackKey]*runtimeCallbackResource)
	}
	if existing := m.callbacks[key]; existing != nil {
		existing.claims[generation] = true
		return existing, nil
	}
	if len(m.callbacks) >= 32 {
		return nil, fmt.Errorf("callback resource limit reached")
	}
	listener, err := net.Listen("tcp", key.listen)
	if err != nil {
		return nil, err
	}
	resource := &runtimeCallbackResource{key: key, address: listener.Addr().String(), claims: map[string]bool{generation: true}, closed: make(chan struct{}), slots: make(chan struct{}, 32)}
	resource.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.forwardRuntimeCallback(resource, w, r) }),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	m.callbacks[key] = resource
	go func() { _ = resource.server.Serve(listener) }()
	go func() {
		select {
		case <-m.runtimeCtx.Done():
			_ = resource.server.Close()
		case <-resource.closed:
		}
	}()
	return resource, nil
}
func (m *Manager) releaseRuntimeResources(inst *PluginInstance) {
	m.callbackMu.Lock()
	defer m.callbackMu.Unlock()
	for _, resource := range inst.callbacks {
		if m.callbacks[resource.key] != resource {
			continue
		}
		delete(resource.claims, inst.Generation)
		if len(resource.claims) > 0 {
			continue
		}
		delete(m.callbacks, resource.key)
		_ = resource.server.Close()
		close(resource.closed)
	}
}
func (m *Manager) forwardRuntimeCallback(resource *runtimeCallbackResource, w http.ResponseWriter, r *http.Request) {
	select {
	case resource.slots <- struct{}{}:
		defer func() { <-resource.slots }()
	default:
		http.Error(w, "callback busy", http.StatusServiceUnavailable)
		return
	}
	if len(r.URL.RequestURI()) > 8192 {
		http.Error(w, "callback URI too large", http.StatusRequestURITooLong)
		return
	}
	inst, release, err := m.GetInstance(resource.key.plugin).Acquire()
	if err != nil {
		http.Error(w, "callback unavailable", http.StatusServiceUnavailable)
		return
	}
	defer release()
	declared := false
	for _, binding := range inst.callbacks {
		if binding == resource {
			declared = true
			break
		}
	}
	if !declared || inst.runtime == nil {
		http.Error(w, "callback unavailable", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "callback body too large", http.StatusRequestEntityTooLarge)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	response, err := inst.runtime.HandleRuntimeCallback(ctx, sdk.CallbackRequest{Name: resource.key.name, Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Headers: r.Header.Clone(), Body: body})
	if err != nil || response.Status < 100 || response.Status > 599 || len(response.Body) > 64<<10 {
		http.Error(w, "callback failed", http.StatusBadGateway)
		return
	}
	for key, values := range response.Headers {
		if http.CanonicalHeaderKey(key) == "Connection" || http.CanonicalHeaderKey(key) == "Transfer-Encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}
