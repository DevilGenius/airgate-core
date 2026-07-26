package monitor

import (
	"context"
	"log/slog"
	"time"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

const (
	autoResolveInterval = 5 * time.Minute
	cleanupInterval     = time.Hour
	recoveryInterval    = 30 * time.Second
	workerRunTimeout    = 30 * time.Second
	workerBatchSize     = 500
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
	s.loadRecoverySnapshot(ctx)
	s.runAutoResolveOnce(ctx)
	s.runCleanupExpiredOnce(ctx)

	autoTicker := time.NewTicker(autoResolveInterval)
	defer autoTicker.Stop()
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer cleanupTicker.Stop()
	recoveryTicker := time.NewTicker(recoveryInterval)
	defer recoveryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-autoTicker.C:
			s.runAutoResolveOnce(ctx)
		case <-cleanupTicker.C:
			s.runCleanupExpiredOnce(ctx)
		case <-recoveryTicker.C:
			s.loadRecoverySnapshot(ctx)
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
				Type:        monitoring.TypeSystemError,
				Severity:    monitoring.SeverityWarning,
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
		s.broadcastMonitorChanged("auto_resolved")
	}
}

func (s *Service) runCleanupExpiredOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, workerRunTimeout)
	defer cancel()

	total := 0
	requestTotal := 0
	requestTraceTotal := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		deleted, err := s.repo.CleanupExpired(ctx, time.Now(), workerBatchSize)
		if err != nil {
			slog.Warn("monitor_cleanup_failed", "deleted", total, "error", err)
			s.Record(context.Background(), monitoring.EventInput{
				Type:        monitoring.TypeSystemError,
				Severity:    monitoring.SeverityWarning,
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
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		deleted, err := s.repo.CleanupExpiredRequests(ctx, time.Now(), workerBatchSize)
		if err != nil {
			slog.Warn("monitor_request_cleanup_failed", "deleted", requestTotal, "error", err)
			s.Record(context.Background(), monitoring.EventInput{
				Type:        monitoring.TypeSystemError,
				Severity:    monitoring.SeverityWarning,
				Source:      monitoring.SourceMonitorWorker,
				SubjectType: monitoring.SubjectSystem,
				SubjectID:   "request_cleanup",
				Title:       "Monitor request cleanup failed",
				Message:     err.Error(),
				ErrorCode:   "monitor_request_cleanup_failed",
			})
			return
		}
		requestTotal += deleted
		if deleted < workerBatchSize {
			break
		}
	}
	if traceRepo, ok := s.repo.(RequestTraceRepository); ok {
		for {
			if err := ctx.Err(); err != nil {
				return
			}
			deleted, err := traceRepo.CleanupExpiredRequestTraces(ctx, time.Now(), workerBatchSize)
			if err != nil {
				slog.Warn("monitor_request_trace_cleanup_failed", "deleted", requestTraceTotal, "error", err)
				s.Record(context.Background(), monitoring.EventInput{
					Type:        monitoring.TypeSystemError,
					Severity:    monitoring.SeverityWarning,
					Source:      monitoring.SourceMonitorWorker,
					SubjectType: monitoring.SubjectSystem,
					SubjectID:   "request_trace_cleanup",
					Title:       "Monitor request trace cleanup failed",
					Message:     err.Error(),
					ErrorCode:   "monitor_request_trace_cleanup_failed",
				})
				return
			}
			requestTraceTotal += deleted
			if deleted < workerBatchSize {
				break
			}
		}
	}
	if total > 0 || requestTotal > 0 || requestTraceTotal > 0 {
		slog.Info("monitor_cleanup_completed", "deleted", total, "request_deleted", requestTotal, "request_trace_deleted", requestTraceTotal)
		s.broadcastMonitorChanged("cleanup")
	}
	s.pruneRecoverySnapshot(ctx)
}
