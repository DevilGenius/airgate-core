package handler

import (
	"time"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func toMonitorEventResp(item appmonitor.Event) dto.MonitorEventResp {
	return dto.MonitorEventResp{
		ID:                  item.ID,
		Type:                item.Type,
		Severity:            item.Severity,
		Status:              item.Status,
		RecoveryMode:        item.RecoveryMode,
		Source:              item.Source,
		SubjectType:         item.SubjectType,
		SubjectID:           item.SubjectID,
		Hash:                item.Hash,
		Title:               item.Title,
		Message:             item.Message,
		AccountID:           item.AccountID,
		AccountNameSnapshot: item.AccountNameSnapshot,
		Platform:            item.Platform,
		PluginID:            item.PluginID,
		TaskType:            item.TaskType,
		ErrorCode:           item.ErrorCode,
		CreatedAt:           monitorTimeString(item.CreatedAt),
		UpdatedAt:           monitorTimeString(item.UpdatedAt),
		ResolvedAt:          monitorTimePtrString(item.ResolvedAt),
		AutoResolveAt:       monitorTimePtrString(item.AutoResolveAt),
		ExpiresAt:           monitorTimeString(item.ExpiresAt),
		LastNotifiedAt:      monitorTimePtrString(item.LastNotifiedAt),
		NextNotifyAt:        monitorTimePtrString(item.NextNotifyAt),
		NotifyError:         item.NotifyError,
		Detail:              item.Detail,
	}
}

func toMonitorListResp(result appmonitor.ListResult) dto.MonitorListResp {
	items := make([]dto.MonitorEventResp, 0, len(result.List))
	for _, item := range result.List {
		items = append(items, toMonitorEventResp(item))
	}
	return dto.MonitorListResp{
		List:       items,
		HasMore:    result.HasMore,
		NextCursor: toMonitorCursorResp(result.NextCursor),
	}
}

func toMonitorSummaryResp(item appmonitor.Summary) dto.MonitorSummaryResp {
	return dto.MonitorSummaryResp{
		ActiveTotal:         item.ActiveTotal,
		CriticalTotal:       item.CriticalTotal,
		CriticalActiveTotal: item.CriticalActiveTotal,
		ErrorTotal:          item.ErrorTotal,
		ErrorActiveTotal:    item.ErrorActiveTotal,
		WarningTotal:        item.WarningTotal,
		WarningActiveTotal:  item.WarningActiveTotal,
		InfoTotal:           item.InfoTotal,
		InfoActiveTotal:     item.InfoActiveTotal,
		ByType:              toMonitorTypeCounts(item.ByType),
		TopAccounts:         toMonitorSubjectCounts(item.TopAccounts),
		Recent:              toMonitorEventRespList(item.Recent),
	}
}

func toMonitorRequestEventResp(item appmonitor.RequestEvent) dto.MonitorRequestEventResp {
	return dto.MonitorRequestEventResp{
		ID:                  item.ID,
		Type:                item.Type,
		Severity:            item.Severity,
		Source:              item.Source,
		Hash:                item.Hash,
		Fingerprint:         item.Fingerprint,
		Title:               item.Title,
		Message:             item.Message,
		RequestID:           item.RequestID,
		APIKeyID:            item.APIKeyID,
		APIKeyNameSnapshot:  item.APIKeyNameSnapshot,
		UserID:              item.UserID,
		UserEmailSnapshot:   item.UserEmailSnapshot,
		GroupID:             item.GroupID,
		AccountID:           item.AccountID,
		AccountNameSnapshot: item.AccountNameSnapshot,
		Platform:            item.Platform,
		PluginID:            item.PluginID,
		Method:              item.Method,
		Endpoint:            item.Endpoint,
		Model:               item.Model,
		HTTPStatus:          item.HTTPStatus,
		UpstreamStatus:      item.UpstreamStatus,
		ErrorCode:           item.ErrorCode,
		DurationMS:          item.DurationMS,
		CreatedAt:           monitorTimeString(item.CreatedAt),
		ExpiresAt:           monitorTimeString(item.ExpiresAt),
		Detail:              item.Detail,
	}
}

func toMonitorRequestListResp(result appmonitor.RequestListResult) dto.MonitorRequestListResp {
	items := make([]dto.MonitorRequestEventResp, 0, len(result.List))
	for _, item := range result.List {
		items = append(items, toMonitorRequestEventResp(item))
	}
	return dto.MonitorRequestListResp{
		List:       items,
		HasMore:    result.HasMore,
		NextCursor: toMonitorRequestCursorResp(result.NextCursor),
	}
}

func toMonitorRequestCursorResp(cursor *appmonitor.RequestListCursor) *dto.MonitorRequestCursorResp {
	if cursor == nil {
		return nil
	}
	return &dto.MonitorRequestCursorResp{
		CreatedAt: monitorTimeString(cursor.CreatedAt),
		ID:        cursor.ID,
	}
}

func toMonitorEventRespList(items []appmonitor.Event) []dto.MonitorEventResp {
	out := make([]dto.MonitorEventResp, 0, len(items))
	for _, item := range items {
		out = append(out, toMonitorEventResp(item))
	}
	return out
}

func toMonitorTypeCounts(items []appmonitor.TypeCount) []dto.MonitorTypeCountResp {
	out := make([]dto.MonitorTypeCountResp, 0, len(items))
	for _, item := range items {
		out = append(out, dto.MonitorTypeCountResp{
			Type:  item.Type,
			Count: item.Count,
		})
	}
	return out
}

func toMonitorSubjectCounts(items []appmonitor.SubjectCount) []dto.MonitorSubjectCountResp {
	out := make([]dto.MonitorSubjectCountResp, 0, len(items))
	for _, item := range items {
		out = append(out, dto.MonitorSubjectCountResp{
			ID:    item.ID,
			Name:  item.Name,
			Count: item.Count,
		})
	}
	return out
}

func toMonitorCursorResp(cursor *appmonitor.ListCursor) *dto.MonitorCursorResp {
	if cursor == nil {
		return nil
	}
	return &dto.MonitorCursorResp{
		UpdatedAt: monitorTimeString(cursor.UpdatedAt),
		ID:        cursor.ID,
	}
}

func monitorTimeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func monitorTimePtrString(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	value := monitorTimeString(*t)
	return &value
}
