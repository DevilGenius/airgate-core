package account

import (
	"context"
	"strconv"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

func (s *Service) recordQuotaRefreshFailure(ctx context.Context, item Account, code string, err error, severity string) {
	if s == nil || s.monitor == nil || item.ID <= 0 {
		return
	}
	accountID := item.ID
	message := ""
	if err != nil {
		message = err.Error()
	}
	s.monitor.Record(ctx, monitoring.EventInput{
		Kind:                monitoring.KindUpstreamAccountError,
		Severity:            severity,
		Source:              monitoring.SourceQuotaRefresh,
		SubjectType:         monitoring.SubjectAccount,
		SubjectID:           strconv.Itoa(item.ID),
		AccountID:           &accountID,
		AccountNameSnapshot: item.Name,
		Platform:            item.Platform,
		ErrorCode:           code,
		ErrorType:           "quota_refresh",
		Title:               "Account quota refresh failed",
		Message:             message,
		Detail: map[string]interface{}{
			"account_type": item.Type,
			"state":        item.State,
			"error_code":   code,
		},
	})
}

func (s *Service) recordConnectivityTestFailure(ctx context.Context, item Account, modelID string, code string, err error) {
	if s == nil || s.monitor == nil || item.ID <= 0 {
		return
	}
	accountID := item.ID
	message := ""
	if err != nil {
		message = err.Error()
	}
	s.monitor.Record(ctx, monitoring.EventInput{
		Kind:                monitoring.KindUpstreamAccountError,
		Severity:            monitoring.SeverityError,
		Source:              monitoring.SourceAccountChecker,
		SubjectType:         monitoring.SubjectAccount,
		SubjectID:           strconv.Itoa(item.ID),
		AccountID:           &accountID,
		AccountNameSnapshot: item.Name,
		Platform:            item.Platform,
		Model:               modelID,
		ErrorCode:           code,
		ErrorType:           "connectivity_test",
		Title:               "Account connectivity test failed",
		Message:             message,
		Detail: map[string]interface{}{
			"account_type": item.Type,
			"state":        item.State,
			"model":        modelID,
			"error_code":   code,
		},
	})
}

func (s *Service) resolveAccountMonitorEvents(ctx context.Context, accountID int) {
	if s == nil || s.monitor == nil || accountID <= 0 {
		return
	}
	s.monitor.ResolveBySubject(ctx, monitoring.ResolveQuery{
		Kind:        monitoring.KindUpstreamAccountError,
		SubjectType: monitoring.SubjectAccount,
		SubjectID:   strconv.Itoa(accountID),
		AccountID:   &accountID,
	})
}
