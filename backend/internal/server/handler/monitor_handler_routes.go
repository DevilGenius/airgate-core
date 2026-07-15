package handler

import (
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

// GetMonitorRequestTraceState returns the current-instance runtime trace state.
func (h *MonitorHandler) GetMonitorRequestTraceState(c *gin.Context) {
	runtime := h.requestTraceRuntime()
	if runtime == nil {
		response.Error(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, "请求追踪运行时不可用")
		return
	}
	response.Success(c, dto.MonitorRequestTraceStateResp{Enabled: runtime.RequestTraceEnabled()})
}

// UpdateMonitorRequestTraceState changes tracing for new requests immediately.
func (h *MonitorHandler) UpdateMonitorRequestTraceState(c *gin.Context) {
	var input dto.MonitorRequestTraceUpdateReq
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BindError(c, err)
		return
	}
	runtime := h.requestTraceRuntime()
	if runtime == nil {
		response.Error(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, "请求追踪运行时不可用")
		return
	}
	runtime.SetRequestTraceEnabled(input.Enabled)
	response.Success(c, dto.MonitorRequestTraceStateResp{Enabled: runtime.RequestTraceEnabled()})
}

// GetMonitorRequestTrace returns one verified raw final-error trace by hash.
func (h *MonitorHandler) GetMonitorRequestTrace(c *gin.Context) {
	hash := strings.ToLower(strings.TrimSpace(c.Param("hash")))
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 16 {
		response.BadRequest(c, "无效的请求追踪 hash")
		return
	}
	item, err := h.service.GetRequestTrace(c.Request.Context(), hash)
	if err != nil {
		httpCode, message := handleMonitorError("查询请求追踪失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMonitorRequestTraceResp(item))
}

// MonitorSummary returns active monitor event aggregates.
func (h *MonitorHandler) MonitorSummary(c *gin.Context) {
	result, err := h.service.Summary(c.Request.Context())
	if err != nil {
		httpCode, message := handleMonitorError("查询监控概览失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMonitorSummaryResp(result))
}

// MonitorRequestSummary returns request monitor event aggregates.
func (h *MonitorHandler) MonitorRequestSummary(c *gin.Context) {
	result, err := h.service.RequestSummary(c.Request.Context())
	if err != nil {
		httpCode, message := handleMonitorError("查询请求监控概览失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMonitorSummaryResp(result))
}

// MonitorRuntime returns the latest runtime observability snapshot.
func (h *MonitorHandler) MonitorRuntime(c *gin.Context) {
	if h.runtimeSampler == nil {
		response.Success(c, appmonitor.RuntimeSnapshot{WindowSeconds: 300})
		return
	}
	response.Success(c, h.runtimeSampler.Snapshot())
}

// ListMonitorEvents returns cursor-paged monitor events.
func (h *MonitorHandler) ListMonitorEvents(c *gin.Context) {
	var query dto.MonitorListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	filter, ok := monitorListFilterFromQuery(c, query)
	if !ok {
		return
	}

	result, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		httpCode, message := handleMonitorError("查询监控事件列表失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMonitorListResp(result))
}

// ListMonitorRequestEvents returns cursor-paged request monitor events.
func (h *MonitorHandler) ListMonitorRequestEvents(c *gin.Context) {
	var query dto.MonitorRequestListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}

	filter, ok := monitorRequestListFilterFromQuery(c, query)
	if !ok {
		return
	}

	result, err := h.service.ListRequests(c.Request.Context(), filter)
	if err != nil {
		httpCode, message := handleMonitorError("查询请求监控事件列表失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMonitorRequestListResp(result))
}

// ClearMonitorRequestEvents deletes request monitor events. Without before it clears all request rows.
func (h *MonitorHandler) ClearMonitorRequestEvents(c *gin.Context) {
	before, err := parseMonitorTime(c.Query("before"), false)
	if err != nil {
		response.BadRequest(c, "before 时间格式错误，请使用 RFC3339 或 YYYY-MM-DD")
		return
	}

	deleted, err := h.service.ClearRequestEvents(c.Request.Context(), before)
	if err != nil {
		httpCode, message := handleMonitorError("清理请求监控事件失败", "清理失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, dto.MonitorRequestClearResp{Deleted: deleted})
}

// ClearMonitorRequestTraces deletes all persisted raw request trace payloads.
func (h *MonitorHandler) ClearMonitorRequestTraces(c *gin.Context) {
	deleted, err := h.service.ClearRequestTraces(c.Request.Context())
	if err != nil {
		httpCode, message := handleMonitorError("清理请求追踪失败", "清理失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, dto.MonitorRequestClearResp{Deleted: deleted})
}

// GetMonitorEvent returns one monitor event.
func (h *MonitorHandler) GetMonitorEvent(c *gin.Context) {
	id, err := parseMonitorID(c.Param("id"))
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的监控事件 ID")
		return
	}

	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		httpCode, message := handleMonitorError("查询监控事件失败", "查询失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, toMonitorEventResp(item))
}

// ResolveMonitorEvent marks one monitor event resolved.
func (h *MonitorHandler) ResolveMonitorEvent(c *gin.Context) {
	id, err := parseMonitorID(c.Param("id"))
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的监控事件 ID")
		return
	}

	if err := h.service.Resolve(c.Request.Context(), id); err != nil {
		httpCode, message := handleMonitorError("标记监控事件已恢复失败", "操作失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	response.Success(c, nil)
}

func monitorListFilterFromQuery(c *gin.Context, query dto.MonitorListQuery) (appmonitor.ListFilter, bool) {
	from, err := parseMonitorTime(query.From, false)
	if err != nil {
		response.BadRequest(c, "from 时间格式错误，请使用 RFC3339 或 YYYY-MM-DD")
		return appmonitor.ListFilter{}, false
	}
	to, err := parseMonitorTime(query.To, true)
	if err != nil {
		response.BadRequest(c, "to 时间格式错误，请使用 RFC3339 或 YYYY-MM-DD")
		return appmonitor.ListFilter{}, false
	}

	var cursor *appmonitor.ListCursor
	cursorRaw := query.CursorUpdatedAt
	if cursorRaw == "" {
		cursorRaw = query.Cursor
	}
	if cursorRaw != "" || query.CursorID > 0 {
		cursorTime, err := parseMonitorTime(cursorRaw, false)
		if err != nil || cursorTime == nil || query.CursorID <= 0 {
			response.BadRequest(c, "cursor 参数无效")
			return appmonitor.ListFilter{}, false
		}
		cursor = &appmonitor.ListCursor{
			UpdatedAt: *cursorTime,
			ID:        query.CursorID,
		}
	}

	return appmonitor.ListFilter{
		Status:      query.Status,
		Severity:    query.Severity,
		Type:        query.Type,
		Source:      query.Source,
		SubjectType: query.SubjectType,
		AccountID:   query.AccountID,
		Platform:    query.Platform,
		PluginID:    query.PluginID,
		TaskType:    query.TaskType,
		ErrorCode:   query.ErrorCode,
		From:        from,
		To:          to,
		Limit:       query.Limit,
		Cursor:      cursor,
	}, true
}

func monitorRequestListFilterFromQuery(c *gin.Context, query dto.MonitorRequestListQuery) (appmonitor.RequestListFilter, bool) {
	from, err := parseMonitorTime(query.From, false)
	if err != nil {
		response.BadRequest(c, "from 时间格式错误，请使用 RFC3339 或 YYYY-MM-DD")
		return appmonitor.RequestListFilter{}, false
	}
	to, err := parseMonitorTime(query.To, true)
	if err != nil {
		response.BadRequest(c, "to 时间格式错误，请使用 RFC3339 或 YYYY-MM-DD")
		return appmonitor.RequestListFilter{}, false
	}

	var cursor *appmonitor.RequestListCursor
	cursorRaw := query.CursorCreatedAt
	if cursorRaw == "" {
		cursorRaw = query.Cursor
	}
	if cursorRaw != "" || query.CursorID > 0 {
		cursorTime, err := parseMonitorTime(cursorRaw, false)
		if err != nil || cursorTime == nil || query.CursorID <= 0 {
			response.BadRequest(c, "cursor 参数无效")
			return appmonitor.RequestListFilter{}, false
		}
		cursor = &appmonitor.RequestListCursor{
			CreatedAt: *cursorTime,
			ID:        query.CursorID,
		}
	}

	return appmonitor.RequestListFilter{
		Severity:       query.Severity,
		Type:           query.Type,
		Source:         query.Source,
		APIKeyID:       query.APIKeyID,
		GroupID:        query.GroupID,
		AccountID:      query.AccountID,
		Platform:       query.Platform,
		PluginID:       query.PluginID,
		Method:         query.Method,
		Endpoint:       query.Endpoint,
		Model:          query.Model,
		HTTPStatus:     query.HTTPStatus,
		UpstreamStatus: query.UpstreamStatus,
		ErrorCode:      query.ErrorCode,
		From:           from,
		To:             to,
		Limit:          query.Limit,
		Cursor:         cursor,
	}, true
}
