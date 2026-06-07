package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

const (
	autoResolveInterval = 5 * time.Minute
	cleanupInterval     = time.Hour
	notifyInterval      = 30 * time.Second
	workerRunTimeout    = 30 * time.Second
	notifyRunTimeout    = 2 * time.Minute
	notifySendTimeout   = 15 * time.Second
	workerBatchSize     = 500
	notifyBatchSize     = 20
	notifyLockTTL       = time.Minute
	notifyFailureRetry  = 10 * time.Minute
)

// StartWorkerLoop runs monitor auto-resolve and retention cleanup.
func StartWorkerLoop(ctx context.Context, service *Service) {
	if service != nil {
		service.StartWorkerLoop(ctx)
	}
}

// StartWorkerLoop runs monitor auto-resolve and retention cleanup.
func (s *Service) StartWorkerLoop(ctx context.Context) {
	if s == nil {
		return
	}
	s.superviseLoop(ctx, "monitor_worker", &s.workerPanics, func() {
		s.runWorkerLoop(ctx)
	})
}

func (s *Service) runWorkerLoop(ctx context.Context) {
	if s.repo == nil {
		<-ctx.Done()
		return
	}
	s.runAutoResolveOnce(ctx)
	s.runCleanupExpiredOnce(ctx)
	s.runNotifyOnce(ctx)

	autoTicker := time.NewTicker(autoResolveInterval)
	defer autoTicker.Stop()
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer cleanupTicker.Stop()
	notifyTicker := time.NewTicker(notifyInterval)
	defer notifyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-autoTicker.C:
			s.runAutoResolveOnce(ctx)
		case <-cleanupTicker.C:
			s.runCleanupExpiredOnce(ctx)
		case <-notifyTicker.C:
			s.runNotifyOnce(ctx)
		}
	}
}

func (s *Service) runAutoResolveOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, workerRunTimeout)
	defer cancel()

	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		resolved, err := s.repo.AutoResolveDue(ctx, time.Now(), workerBatchSize)
		if err != nil {
			slog.Warn("monitor_auto_resolve_failed", "resolved", total, "error", err)
			s.Record(context.Background(), monitoring.EventInput{
				Kind:        monitoring.KindSystemError,
				Severity:    monitoring.SeverityError,
				Source:      monitoring.SourceMonitorWorker,
				SubjectType: monitoring.SubjectSystem,
				SubjectID:   "auto_resolve",
				Title:       "Monitor auto resolve failed",
				Message:     err.Error(),
				ErrorCode:   "monitor_auto_resolve_failed",
			})
			return
		}
		total += resolved
		if resolved < workerBatchSize {
			break
		}
	}
	if total > 0 {
		slog.Info("monitor_auto_resolve_completed", "resolved", total)
	}
}

func (s *Service) runCleanupExpiredOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, workerRunTimeout)
	defer cancel()

	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		deleted, err := s.repo.CleanupExpired(ctx, time.Now(), workerBatchSize)
		if err != nil {
			slog.Warn("monitor_cleanup_failed", "deleted", total, "error", err)
			s.Record(context.Background(), monitoring.EventInput{
				Kind:        monitoring.KindSystemError,
				Severity:    monitoring.SeverityError,
				Source:      monitoring.SourceMonitorWorker,
				SubjectType: monitoring.SubjectSystem,
				SubjectID:   "cleanup",
				Title:       "Monitor cleanup failed",
				Message:     err.Error(),
				ErrorCode:   "monitor_cleanup_failed",
			})
			return
		}
		total += deleted
		if deleted < workerBatchSize {
			break
		}
	}
	if total > 0 {
		slog.Info("monitor_cleanup_completed", "deleted", total)
	}
}

func (s *Service) runNotifyOnce(parent context.Context) {
	if s.repo == nil || s.notifier == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, notifyRunTimeout)
	defer cancel()

	configured, err := s.notifier.IsConfigured(ctx)
	if err != nil {
		slog.Warn("monitor_notification_config_check_failed", "error", err)
		return
	}
	if !configured {
		return
	}

	events, err := s.repo.ListNotifyDue(ctx, time.Now(), notifyBatchSize)
	if err != nil {
		slog.Warn("monitor_notification_scan_failed", "error", err)
		return
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return
		}
		claimed, token := s.claimNotify(ctx, event.ID)
		if !claimed {
			continue
		}
		sendCtx, sendCancel := context.WithTimeout(ctx, notifySendTimeout)
		err := s.notifier.Send(sendCtx, monitorNotificationValues(event))
		sendCancel()
		s.releaseNotify(ctx, event.ID, token)

		now := time.Now()
		if err != nil {
			if markErr := s.repo.MarkNotifyFailed(ctx, event.ID, now.Add(notifyFailureRetry), err.Error()); markErr != nil {
				slog.Warn("monitor_notification_failure_mark_failed", "event_id", event.ID, "error", markErr)
			}
			continue
		}
		if err := s.repo.MarkNotified(ctx, event.ID, now, now.Add(notificationCooldown(event.Severity))); err != nil {
			slog.Warn("monitor_notification_success_mark_failed", "event_id", event.ID, "error", err)
		}
	}
}

func (s *Service) claimNotify(ctx context.Context, id int) (bool, string) {
	if s.rdb == nil {
		return true, ""
	}
	token := uuid.NewString()
	ok, err := s.rdb.SetNX(ctx, monitorNotifyLockKey(id), token, notifyLockTTL).Result()
	if err != nil {
		slog.Warn("monitor_notification_claim_failed", "event_id", id, "error", err)
		return false, ""
	}
	return ok, token
}

func (s *Service) releaseNotify(ctx context.Context, id int, token string) {
	if s.rdb == nil || token == "" {
		return
	}
	_, _ = monitorNotifyUnlockScript.Run(ctx, s.rdb, []string{monitorNotifyLockKey(id)}, token).Result()
}

func monitorNotifyLockKey(id int) string {
	return fmt.Sprintf("monitor:notify:%d", id)
}

func notificationCooldown(severity string) time.Duration {
	if severity == monitoring.SeverityCritical {
		return 10 * time.Minute
	}
	return 30 * time.Minute
}

func monitorNotificationValues(event Event) map[string]string {
	subject := event.SubjectID
	switch {
	case event.APIKeyNameSnapshot != "":
		subject = event.APIKeyNameSnapshot
	case event.AccountNameSnapshot != "":
		subject = event.AccountNameSnapshot
	case event.PluginID != "":
		subject = event.PluginID
	}
	content := event.Message
	if content == "" {
		content = event.Title
	}
	return map[string]string{
		"title":                "[AirGate][" + event.Severity + "] " + event.Title,
		"content":              content,
		"severity":             event.Severity,
		"kind":                 event.Kind,
		"status":               event.Status,
		"source":               event.Source,
		"subject_type":         event.SubjectType,
		"subject_id":           event.SubjectID,
		"subject":              subject,
		"message":              event.Message,
		"count":                strconv.FormatInt(event.Count, 10),
		"platform":             event.Platform,
		"plugin_id":            event.PluginID,
		"task_type":            event.TaskType,
		"api_key_id":           intToString(event.APIKeyID),
		"api_key_name":         event.APIKeyNameSnapshot,
		"account_id":           intToString(event.AccountID),
		"account_name":         event.AccountNameSnapshot,
		"endpoint":             event.Endpoint,
		"model":                event.Model,
		"http_status":          intToString(event.HTTPStatus),
		"upstream_status":      intToString(event.UpstreamStatus),
		"error_code":           event.ErrorCode,
		"error_type":           event.ErrorType,
		"created_at":           event.CreatedAt.Format(time.RFC3339),
		"updated_at":           event.UpdatedAt.Format(time.RFC3339),
		"last_notified_at":     timePtrToString(event.LastNotifiedAt),
		"next_notify_at":       timePtrToString(event.NextNotifyAt),
		"monitor_event_id":     strconv.Itoa(event.ID),
		"monitor_event_status": event.Status,
	}
}

func intToString(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func timePtrToString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
