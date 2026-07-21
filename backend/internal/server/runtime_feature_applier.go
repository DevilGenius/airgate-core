package server

import (
	"context"
	"log/slog"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/runtimefeatures"
)

type runtimeFeatureApplier struct {
	monitor   *appmonitor.Service
	forwarder *plugin.Forwarder
	plugins   *plugin.Manager
}

func newRuntimeFeatureApplier(
	monitor *appmonitor.Service,
	forwarder *plugin.Forwarder,
	plugins *plugin.Manager,
) *runtimeFeatureApplier {
	return &runtimeFeatureApplier{
		monitor:   monitor,
		forwarder: forwarder,
		plugins:   plugins,
	}
}

func (a *runtimeFeatureApplier) ApplyRuntimeFeatures(ctx context.Context, state runtimefeatures.State) error {
	if a == nil {
		return nil
	}
	if a.plugins != nil {
		current := a.plugins.RuntimeHashState()
		next := plugin.RuntimeHashState{
			TextEnabled:  state.TextHashEnabled,
			ImageEnabled: state.ImageHashEnabled,
		}
		if current != next {
			if err := a.plugins.SetRuntimeHashState(ctx, next); err != nil {
				return err
			}
		}
	}

	monitorEnabled := a.monitor != nil && a.monitor.RequestTraceEnabled()
	forwarderEnabled := a.forwarder != nil && a.forwarder.RequestTraceEnabled()
	if monitorEnabled == state.RequestTraceEnabled && forwarderEnabled == state.RequestTraceEnabled {
		return nil
	}
	if state.RequestTraceEnabled {
		if a.monitor != nil {
			a.monitor.SetRequestTraceEnabled(true)
		}
		if a.forwarder != nil {
			a.forwarder.SetRequestTraceEnabled(true)
		}
		slog.Warn("monitor_request_trace_enabled", "retention", "7d", "raw_request_bodies", true, "source", "system_settings")
		return nil
	}
	if a.forwarder != nil {
		a.forwarder.SetRequestTraceEnabled(false)
	}
	if a.monitor != nil {
		a.monitor.SetRequestTraceEnabled(false)
	}
	slog.Info("monitor_request_trace_disabled", "source", "system_settings")
	return nil
}
