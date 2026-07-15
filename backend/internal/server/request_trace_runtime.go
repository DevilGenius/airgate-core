package server

import (
	"log/slog"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/plugin"
)

type requestTraceRuntime struct {
	monitor   *appmonitor.Service
	forwarder *plugin.Forwarder
}

func newRequestTraceRuntime(monitor *appmonitor.Service, forwarder *plugin.Forwarder) *requestTraceRuntime {
	return &requestTraceRuntime{monitor: monitor, forwarder: forwarder}
}

func (r *requestTraceRuntime) RequestTraceEnabled() bool {
	return r != nil && r.monitor != nil && r.monitor.RequestTraceEnabled()
}

func (r *requestTraceRuntime) SetRequestTraceEnabled(enabled bool) {
	if r == nil {
		return
	}
	if enabled {
		if r.monitor != nil {
			r.monitor.SetRequestTraceEnabled(true)
		}
		if r.forwarder != nil {
			r.forwarder.SetRequestTraceEnabled(true)
		}
		slog.Warn("monitor_request_trace_enabled", "retention", "7d", "raw_request_bodies", true, "source", "admin_runtime")
		return
	}
	if r.forwarder != nil {
		r.forwarder.SetRequestTraceEnabled(false)
	}
	if r.monitor != nil {
		r.monitor.SetRequestTraceEnabled(false)
	}
	slog.Info("monitor_request_trace_disabled", "source", "admin_runtime")
}
