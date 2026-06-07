package handler

import (
	"time"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func toMonitorEventResp(item appmonitor.Event) dto.MonitorEventResp {
	return dto.MonitorEventResp{
		ID:                  item.ID,
		Kind:                item.Kind,
		Severity:            item.Severity,
		Status:              item.Status,
		Source:              item.Source,
		SubjectType:         item.SubjectType,
		SubjectID:           item.SubjectID,
		Fingerprint:         item.Fingerprint,
		Title:               item.Title,
		Message:             item.Message,
		APIKeyID:            item.APIKeyID,
		APIKeyNameSnapshot:  item.APIKeyNameSnapshot,
		APIKeyPrefix:        item.APIKeyPrefix,
		UserID:              item.UserID,
		UserEmailSnapshot:   item.UserEmailSnapshot,
		GroupID:             item.GroupID,
		AccountID:           item.AccountID,
		AccountNameSnapshot: item.AccountNameSnapshot,
		Platform:            item.Platform,
		PluginID:            item.PluginID,
		TaskType:            item.TaskType,
		Method:              item.Method,
		Endpoint:            item.Endpoint,
		RequestPath:         item.RequestPath,
		Model:               item.Model,
		HTTPStatus:          item.HTTPStatus,
		UpstreamStatus:      item.UpstreamStatus,
		ErrorCode:           item.ErrorCode,
		ErrorType:           item.ErrorType,
		Count:               item.Count,
		CreatedAt:           monitorTimeString(item.CreatedAt),
		UpdatedAt:           monitorTimeString(item.UpdatedAt),
		ResolvedAt:          monitorTimePtrString(item.ResolvedAt),
		IgnoredAt:           monitorTimePtrString(item.IgnoredAt),
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
		ActiveTotal:   item.ActiveTotal,
		CriticalTotal: item.CriticalTotal,
		ErrorTotal:    item.ErrorTotal,
		WarningTotal:  item.WarningTotal,
		ByKind:        toMonitorKindCounts(item.ByKind),
		TopAPIKeys:    toMonitorSubjectCounts(item.TopAPIKeys),
		TopAccounts:   toMonitorSubjectCounts(item.TopAccounts),
		Recent:        toMonitorEventRespList(item.Recent),
	}
}

func toMonitorEventRespList(items []appmonitor.Event) []dto.MonitorEventResp {
	out := make([]dto.MonitorEventResp, 0, len(items))
	for _, item := range items {
		out = append(out, toMonitorEventResp(item))
	}
	return out
}

func toMonitorKindCounts(items []appmonitor.KindCount) []dto.MonitorKindCountResp {
	out := make([]dto.MonitorKindCountResp, 0, len(items))
	for _, item := range items {
		out = append(out, dto.MonitorKindCountResp{
			Kind:  item.Kind,
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
