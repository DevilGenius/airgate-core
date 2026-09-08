package plugin

import (
	"context"
	"log/slog"
	"time"
)

func (m *Manager) beginRuntimeDrain(inst *PluginInstance, parent context.Context) {
	inst.drainOnce.Do(func() {
		client := inst.runtimeClient()
		if client == nil {
			return
		}
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		if err := client.BeginDrain(ctx); err != nil {
			slog.Warn("plugin_drain_hook_failed", "plugin", inst.Name, "error", err)
		}
	})
}
