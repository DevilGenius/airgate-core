package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
)

const (
	runtimeHashPath          = "runtime/hash"
	runtimeHashUpdateTimeout = 5 * time.Second
)

type RuntimeHashState struct {
	TextEnabled  bool `json:"text_enabled"`
	ImageEnabled bool `json:"image_enabled"`
}

func DefaultRuntimeHashState() RuntimeHashState {
	return RuntimeHashState{TextEnabled: true, ImageEnabled: true}
}

func (m *Manager) RuntimeHashState() RuntimeHashState {
	if m == nil {
		return DefaultRuntimeHashState()
	}
	m.runtimeHashMu.RLock()
	defer m.runtimeHashMu.RUnlock()
	return m.runtimeHashStateLocked()
}

func (m *Manager) SetRuntimeHashState(ctx context.Context, state RuntimeHashState) error {
	if m == nil {
		return fmt.Errorf("plugin manager is unavailable")
	}
	m.runtimeHashMu.Lock()
	defer m.runtimeHashMu.Unlock()

	m.mu.RLock()
	var gateway *sdkgrpc.GatewayGRPCClient
	for _, instance := range m.instances {
		if instance != nil && instance.Platform == "openai" && instance.Gateway != nil {
			gateway = instance.Gateway
			break
		}
	}
	if gateway != nil {
		err := applyRuntimeHashState(ctx, gateway, state)
		m.mu.RUnlock()
		if err != nil {
			return err
		}
	} else {
		m.mu.RUnlock()
	}
	m.runtimeHashState = state
	m.runtimeHashConfigured = true
	return nil
}

// prepareRuntimeHashForPublish holds the desired-state read lock until
// the configured plugin instance is published, preventing a concurrent update
// from being lost during plugin load or hot reload.
func (m *Manager) prepareRuntimeHashForPublish(
	ctx context.Context,
	gateway *sdkgrpc.GatewayGRPCClient,
	platform string,
) (func(), error) {
	if m == nil || gateway == nil || platform != "openai" {
		return func() {}, nil
	}
	m.runtimeHashMu.RLock()
	state := m.runtimeHashStateLocked()
	if err := applyRuntimeHashState(ctx, gateway, state); err != nil {
		m.runtimeHashMu.RUnlock()
		return nil, err
	}
	return m.runtimeHashMu.RUnlock, nil
}

func (m *Manager) runtimeHashStateLocked() RuntimeHashState {
	if !m.runtimeHashConfigured {
		return DefaultRuntimeHashState()
	}
	return m.runtimeHashState
}

func applyRuntimeHashState(
	ctx context.Context,
	gateway *sdkgrpc.GatewayGRPCClient,
	state RuntimeHashState,
) error {
	if gateway == nil {
		return fmt.Errorf("openai gateway is unavailable")
	}
	body, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode runtime hash state: %w", err)
	}
	requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeHashUpdateTimeout)
	defer cancel()
	status, _, responseBody, err := gateway.HandleHTTPRequest(
		requestCtx,
		http.MethodPut,
		runtimeHashPath,
		"application/json",
		nil,
		body,
	)
	if err != nil {
		return fmt.Errorf("update openai runtime hash: %w", err)
	}
	if status != http.StatusOK {
		message := strings.TrimSpace(string(responseBody))
		if len(message) > 512 {
			message = message[:512]
		}
		return fmt.Errorf("update openai runtime hash: status %d: %s", status, message)
	}
	var applied RuntimeHashState
	if err := json.Unmarshal(responseBody, &applied); err != nil {
		return fmt.Errorf("decode openai runtime hash state: %w", err)
	}
	if applied != state {
		return fmt.Errorf("openai runtime hash state mismatch: got %+v, want %+v", applied, state)
	}
	return nil
}
