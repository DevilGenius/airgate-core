package handler

import (
	"errors"
	"log/slog"
	"strconv"
	"time"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
)

// MonitorHandler handles admin monitor event APIs.
type MonitorHandler struct {
	service        *appmonitor.Service
	runtimeSampler *appmonitor.RuntimeSampler
	requestTrace   RequestTraceRuntime
}

// RequestTraceRuntime controls final-error tracing for the current Core instance.
type RequestTraceRuntime interface {
	RequestTraceEnabled() bool
	SetRequestTraceEnabled(bool)
}

// NewMonitorHandler creates a MonitorHandler.
func NewMonitorHandler(service *appmonitor.Service, runtimeSampler ...*appmonitor.RuntimeSampler) *MonitorHandler {
	h := &MonitorHandler{service: service}
	if len(runtimeSampler) > 0 {
		h.runtimeSampler = runtimeSampler[0]
	}
	return h
}

// SetRequestTraceRuntime injects the runtime trace controller.
func (h *MonitorHandler) SetRequestTraceRuntime(runtime RequestTraceRuntime) {
	if h == nil {
		return
	}
	h.requestTrace = runtime
}

func (h *MonitorHandler) requestTraceRuntime() RequestTraceRuntime {
	if h == nil {
		return nil
	}
	if h.requestTrace != nil {
		return h.requestTrace
	}
	if h.service != nil {
		return h.service
	}
	return nil
}

func parseMonitorID(raw string) (int, error) {
	return strconv.Atoi(raw)
}

func parseMonitorTime(value string, endOfDay bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return &t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return nil, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return &t, nil
}

func handleMonitorError(logMessage, publicMessage string, err error) (int, string) {
	if errors.Is(err, appmonitor.ErrEventNotFound) || errors.Is(err, appmonitor.ErrRequestTraceNotFound) {
		return 404, err.Error()
	}
	if errors.Is(err, appmonitor.ErrEventNotRecoverable) {
		return 409, err.Error()
	}
	slog.Error(logMessage, "error", err)
	return 500, publicMessage
}
