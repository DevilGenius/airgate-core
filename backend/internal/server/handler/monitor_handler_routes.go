package handler

import (
	"github.com/gin-gonic/gin"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

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

// IgnoreMonitorEvent marks one monitor event ignored.
func (h *MonitorHandler) IgnoreMonitorEvent(c *gin.Context) {
	id, err := parseMonitorID(c.Param("id"))
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的监控事件 ID")
		return
	}

	if err := h.service.Ignore(c.Request.Context(), id); err != nil {
		httpCode, message := handleMonitorError("忽略监控事件失败", "操作失败", err)
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
		APIKeyID:    query.APIKeyID,
		AccountID:   query.AccountID,
		Platform:    query.Platform,
		PluginID:    query.PluginID,
		TaskType:    query.TaskType,
		Endpoint:    query.Endpoint,
		ErrorCode:   query.ErrorCode,
		From:        from,
		To:          to,
		Limit:       query.Limit,
		Cursor:      cursor,
	}, true
}
