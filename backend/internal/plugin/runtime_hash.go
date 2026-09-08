package plugin

import (
	"context"
	"fmt"
	"strconv"
	"time"
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
func (state RuntimeHashState) settings() map[string]string {
	return map[string]string{"text_hash_enabled": strconv.FormatBool(state.TextEnabled), "image_hash_enabled": strconv.FormatBool(state.ImageEnabled)}
}
func (m *Manager) RuntimeHashState() RuntimeHashState {
	if m == nil {
		return DefaultRuntimeHashState()
	}
	m.runtimeHashMu.RLock()
	defer m.runtimeHashMu.RUnlock()
	return m.runtimeHashStateLocked()
}

// Public system settings use the SDK runtime settings contract for every
// plugin. Which settings a provider implements is decided inside that plugin.
func (m *Manager) SetRuntimeHashState(ctx context.Context, state RuntimeHashState) error {
	if m == nil {
		return fmt.Errorf("plugin manager unavailable")
	}
	m.runtimeHashMu.Lock()
	defer m.runtimeHashMu.Unlock()
	m.mu.RLock()
	var targets []*PluginInstance
	for _, inst := range m.instances {
		if inst.runtimeClient() != nil && inst.acquireRequest() {
			targets = append(targets, inst)
		}
	}
	m.mu.RUnlock()
	defer func() {
		for _, inst := range targets {
			inst.releaseRequest()
		}
	}()
	requestCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeHashUpdateTimeout)
	defer cancel()
	for _, inst := range targets {
		if err := inst.runtimeClient().ApplyRuntimeSettings(requestCtx, state.settings()); err != nil {
			return err
		}
	}
	m.runtimeHashState, m.runtimeHashConfigured = state, true
	return nil
}
func (m *Manager) prepareRuntimeSettingsForPublish(ctx context.Context, client lifecycleClient) (func(), error) {
	if client == nil {
		return func() {}, nil
	}
	m.runtimeHashMu.RLock()
	if err := client.ApplyRuntimeSettings(ctx, m.runtimeHashStateLocked().settings()); err != nil {
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
