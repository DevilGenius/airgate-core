package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

func TestMonitorRouteSuccessBranches(t *testing.T) {
	now := time.Date(2026, 6, 20, 1, 2, 3, 0, time.UTC)
	repo := &handlerMonitorRepoStub{
		summary: func(context.Context) (appmonitor.Summary, error) {
			return appmonitor.Summary{ActiveTotal: 2}, nil
		},
		requestSummary: func(context.Context) (appmonitor.Summary, error) {
			return appmonitor.Summary{WarningTotal: 3}, nil
		},
		list: func(_ context.Context, filter appmonitor.ListFilter) (appmonitor.ListResult, error) {
			if filter.Limit != 25 || filter.Status != "active" || filter.Cursor == nil || filter.Cursor.ID != 9 {
				t.Fatalf("monitor list filter = %+v", filter)
			}
			return appmonitor.ListResult{List: []appmonitor.Event{{ID: 1, Type: monitoring.TypeSystemError, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}}}, nil
		},
		listRequests: func(_ context.Context, filter appmonitor.RequestListFilter) (appmonitor.RequestListResult, error) {
			if filter.Limit != 20 || filter.Cursor == nil || filter.Cursor.ID != 8 {
				t.Fatalf("request monitor list filter = %+v", filter)
			}
			return appmonitor.RequestListResult{List: []appmonitor.RequestEvent{{ID: 2, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}}, nil
		},
		clearRequestEvents: func(_ context.Context, before *time.Time) (int, error) {
			if before == nil {
				t.Fatal("expected before filter")
			}
			return 4, nil
		},
		get: func(_ context.Context, id int) (appmonitor.Event, error) {
			if id != 7 {
				t.Fatalf("get id = %d", id)
			}
			return appmonitor.Event{ID: id, Severity: monitoring.SeverityError, RecoveryMode: monitoring.RecoveryModeManual, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour)}, nil
		},
		resolve: func(_ context.Context, id int) error {
			if id != 7 {
				t.Fatalf("resolve id = %d", id)
			}
			return nil
		},
	}
	h := NewMonitorHandler(appmonitor.NewService(repo))

	tests := []struct {
		name     string
		method   string
		target   string
		params   map[string]string
		fn       func(*ginContextAlias)
		contains string
	}{
		{name: "summary", method: http.MethodGet, target: "/monitor/summary", fn: h.MonitorSummary, contains: `"active_total":2`},
		{name: "request summary", method: http.MethodGet, target: "/monitor/requests/summary", fn: h.MonitorRequestSummary, contains: `"warning_total":3`},
		{name: "list", method: http.MethodGet, target: "/monitor?status=active&limit=25&cursor=2026-06-20T00:00:00Z&cursor_id=9", fn: h.ListMonitorEvents, contains: `"id":1`},
		{name: "request list", method: http.MethodGet, target: "/monitor/requests?limit=20&cursor=2026-06-20T00:00:00Z&cursor_id=8", fn: h.ListMonitorRequestEvents, contains: `"id":2`},
		{name: "clear requests", method: http.MethodDelete, target: "/monitor/requests?before=2026-06-20", fn: h.ClearMonitorRequestEvents, contains: `"deleted":4`},
		{name: "get", method: http.MethodGet, target: "/monitor/7", params: map[string]string{"id": "7"}, fn: h.GetMonitorEvent, contains: `"id":7`},
		{name: "resolve", method: http.MethodPatch, target: "/monitor/7/resolve", params: map[string]string{"id": "7"}, fn: h.ResolveMonitorEvent, contains: `"message":"ok"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := ginParamsFromMap(tt.params)
			w := invokeHandlerForValidation(tt.method, tt.target, "", params, nil, func(c *ginContextAlias) {
				tt.fn(c)
			})
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tt.contains) {
				t.Fatalf("status/body = %d %s, want %q", w.Code, w.Body.String(), tt.contains)
			}
		})
	}
}

func TestMonitorRouteErrorBranches(t *testing.T) {
	errBoom := errors.New("boom")
	h := NewMonitorHandler(appmonitor.NewService(&handlerMonitorRepoStub{
		summary:        func(context.Context) (appmonitor.Summary, error) { return appmonitor.Summary{}, errBoom },
		requestSummary: func(context.Context) (appmonitor.Summary, error) { return appmonitor.Summary{}, errBoom },
		list: func(context.Context, appmonitor.ListFilter) (appmonitor.ListResult, error) {
			return appmonitor.ListResult{}, errBoom
		},
		listRequests: func(context.Context, appmonitor.RequestListFilter) (appmonitor.RequestListResult, error) {
			return appmonitor.RequestListResult{}, errBoom
		},
		clearRequestEvents: func(context.Context, *time.Time) (int, error) { return 0, errBoom },
		get: func(_ context.Context, id int) (appmonitor.Event, error) {
			if id == 404 {
				return appmonitor.Event{}, appmonitor.ErrEventNotFound
			}
			return appmonitor.Event{ID: id, Severity: monitoring.SeverityError, RecoveryMode: monitoring.RecoveryModeManual}, nil
		},
		resolve: func(context.Context, int) error { return appmonitor.ErrEventNotRecoverable },
	}))

	tests := []struct {
		name   string
		method string
		target string
		params map[string]string
		fn     func(*ginContextAlias)
		status int
	}{
		{name: "summary", method: http.MethodGet, target: "/monitor/summary", fn: h.MonitorSummary, status: http.StatusInternalServerError},
		{name: "request summary", method: http.MethodGet, target: "/monitor/requests/summary", fn: h.MonitorRequestSummary, status: http.StatusInternalServerError},
		{name: "list", method: http.MethodGet, target: "/monitor", fn: h.ListMonitorEvents, status: http.StatusInternalServerError},
		{name: "request list", method: http.MethodGet, target: "/monitor/requests", fn: h.ListMonitorRequestEvents, status: http.StatusInternalServerError},
		{name: "clear requests", method: http.MethodDelete, target: "/monitor/requests", fn: h.ClearMonitorRequestEvents, status: http.StatusInternalServerError},
		{name: "get not found", method: http.MethodGet, target: "/monitor/404", params: map[string]string{"id": "404"}, fn: h.GetMonitorEvent, status: http.StatusNotFound},
		{name: "resolve conflict", method: http.MethodPatch, target: "/monitor/7/resolve", params: map[string]string{"id": "7"}, fn: h.ResolveMonitorEvent, status: http.StatusConflict},
		{name: "clear bad before", method: http.MethodDelete, target: "/monitor/requests?before=bad", fn: h.ClearMonitorRequestEvents, status: http.StatusBadRequest},
		{name: "resolve bad id", method: http.MethodPatch, target: "/monitor/bad/resolve", params: map[string]string{"id": "bad"}, fn: h.ResolveMonitorEvent, status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, "", ginParamsFromMap(tt.params), nil, func(c *ginContextAlias) {
				tt.fn(c)
			})
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

type ginContextAlias = gin.Context

func ginParamsFromMap(values map[string]string) gin.Params {
	if len(values) == 0 {
		return nil
	}
	out := make(gin.Params, 0, len(values))
	for key, value := range values {
		out = append(out, gin.Param{Key: key, Value: value})
	}
	return out
}

type handlerMonitorRepoStub struct {
	summary            func(context.Context) (appmonitor.Summary, error)
	requestSummary     func(context.Context) (appmonitor.Summary, error)
	list               func(context.Context, appmonitor.ListFilter) (appmonitor.ListResult, error)
	listRequests       func(context.Context, appmonitor.RequestListFilter) (appmonitor.RequestListResult, error)
	clearRequestEvents func(context.Context, *time.Time) (int, error)
	get                func(context.Context, int) (appmonitor.Event, error)
	resolve            func(context.Context, int) error
}

func (h *handlerMonitorRepoStub) InsertBatch(context.Context, []appmonitor.QueuedEvent) error {
	return nil
}

func (h *handlerMonitorRepoStub) InsertRequestBatch(context.Context, []appmonitor.QueuedRequestEvent) error {
	return nil
}

func (h *handlerMonitorRepoStub) ResolveBySubject(context.Context, monitoring.ResolveQuery) error {
	return nil
}

func (h *handlerMonitorRepoStub) Get(ctx context.Context, id int) (appmonitor.Event, error) {
	if h.get != nil {
		return h.get(ctx, id)
	}
	return appmonitor.Event{}, appmonitor.ErrEventNotFound
}

func (h *handlerMonitorRepoStub) Resolve(ctx context.Context, id int) error {
	if h.resolve != nil {
		return h.resolve(ctx, id)
	}
	return nil
}

func (h *handlerMonitorRepoStub) List(ctx context.Context, filter appmonitor.ListFilter) (appmonitor.ListResult, error) {
	if h.list != nil {
		return h.list(ctx, filter)
	}
	return appmonitor.ListResult{}, nil
}

func (h *handlerMonitorRepoStub) ListRequests(ctx context.Context, filter appmonitor.RequestListFilter) (appmonitor.RequestListResult, error) {
	if h.listRequests != nil {
		return h.listRequests(ctx, filter)
	}
	return appmonitor.RequestListResult{}, nil
}

func (h *handlerMonitorRepoStub) ClearRequestEvents(ctx context.Context, before *time.Time) (int, error) {
	if h.clearRequestEvents != nil {
		return h.clearRequestEvents(ctx, before)
	}
	return 0, nil
}

func (h *handlerMonitorRepoStub) Summary(ctx context.Context) (appmonitor.Summary, error) {
	if h.summary != nil {
		return h.summary(ctx)
	}
	return appmonitor.Summary{}, nil
}

func (h *handlerMonitorRepoStub) RequestSummary(ctx context.Context) (appmonitor.Summary, error) {
	if h.requestSummary != nil {
		return h.requestSummary(ctx)
	}
	return appmonitor.Summary{}, nil
}

func (h *handlerMonitorRepoStub) CleanupExpired(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

func (h *handlerMonitorRepoStub) CleanupExpiredRequests(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

func (h *handlerMonitorRepoStub) AutoResolveDue(context.Context, time.Time, int) (int, error) {
	return 0, nil
}

func (h *handlerMonitorRepoStub) ListNotifyDue(context.Context, time.Time, int) ([]appmonitor.Event, error) {
	return nil, nil
}

func (h *handlerMonitorRepoStub) MarkNotified(context.Context, int, time.Time, time.Time) error {
	return nil
}

func (h *handlerMonitorRepoStub) MarkNotifyFailed(context.Context, int, time.Time, string) error {
	return nil
}
