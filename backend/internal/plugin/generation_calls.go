package plugin

import (
	"context"
	"errors"
	"net/http"
	"sync"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func (inst *PluginInstance) runtimeClient() lifecycleClient {
	if inst == nil {
		return nil
	}
	if inst.runtime != nil {
		return inst.runtime
	}
	switch {
	case inst.Gateway != nil:
		return inst.Gateway
	case inst.Extension != nil:
		return inst.Extension
	case inst.Middleware != nil:
		return inst.Middleware
	}
	return nil
}

// retainRequest is only legal while holding an existing lease. It transfers
// ownership to a child task without reopening admission to a draining process.
func (inst *PluginInstance) retainRequest() {
	inst.lifecycleMu.Lock()
	inst.activeRequests++
	inst.lifecycleMu.Unlock()
}

func (inst *PluginInstance) cancelBackground() {
	inst.lifecycleMu.Lock()
	cancel := inst.stopBackground
	inst.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (inst *PluginInstance) HandleWebSocket(ctx context.Context, conn sdk.WebSocketConn) (sdk.ForwardOutcome, error) {
	current, release, err := inst.Acquire()
	if err != nil {
		return sdk.ForwardOutcome{}, err
	}
	defer release()
	if current.Gateway == nil {
		return sdk.ForwardOutcome{}, errors.New("gateway unavailable")
	}
	return current.Gateway.HandleWebSocket(ctx, conn)
}

// Acquire chooses and pins the current generation while holding the catalog
// read lock. Even a reference obtained before cutover enters the new generation.
// The caller retains this lease for the complete RPC/stream, including cleanup.
func (inst *PluginInstance) Acquire() (*PluginInstance, func(), error) {
	if inst == nil {
		return nil, nil, errors.New("plugin is not available")
	}
	current := inst
	if inst.owner != nil {
		m := inst.owner
		m.mu.RLock()
		defer m.mu.RUnlock()
		current = m.instances[inst.Name]
		if m.closing || current == nil {
			return nil, nil, errors.New("plugin is stopping")
		}
	}
	if !current.acquireRequest() {
		return nil, nil, errPluginInstanceDraining
	}
	var once sync.Once
	return current, func() { once.Do(current.releaseRequest) }, nil
}

func (inst *PluginInstance) HandleHTTPRequest(ctx context.Context, method, path, query string, headers http.Header, body []byte) (int, http.Header, []byte, error) {
	current, release, err := inst.Acquire()
	if err != nil {
		return 0, nil, nil, err
	}
	defer release()
	if current.Gateway == nil {
		return 0, nil, nil, errors.New("gateway is unavailable")
	}
	return current.Gateway.HandleHTTPRequest(ctx, method, path, query, headers, body)
}

func (inst *PluginInstance) ValidateAccount(ctx context.Context, credentials map[string]string) error {
	current, release, err := inst.Acquire()
	if err != nil {
		return err
	}
	defer release()
	if current.Gateway == nil {
		return errors.New("gateway is unavailable")
	}
	return current.Gateway.ValidateAccount(ctx, credentials)
}

func (inst *PluginInstance) HealthCheck(ctx context.Context) error {
	current, release, err := inst.Acquire()
	if err != nil {
		return err
	}
	defer release()
	switch {
	case current.Gateway != nil:
		return current.Gateway.HealthCheck(ctx)
	case current.Extension != nil:
		return current.Extension.HealthCheck(ctx)
	case current.Middleware != nil:
		return current.Middleware.HealthCheck(ctx)
	}
	return errors.New("plugin has no runtime")
}

func (inst *PluginInstance) UpdateConfig(ctx sdk.PluginContext) error {
	current, release, err := inst.Acquire()
	if err != nil {
		return err
	}
	defer release()
	if current.Gateway == nil {
		return errors.New("gateway is unavailable")
	}
	return current.Gateway.UpdateConfig(ctx)
}
