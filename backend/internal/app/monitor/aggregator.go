package monitor

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

const (
	loopRestartDelay = time.Second
	tailFlushTimeout = 5 * time.Second
)

// StartAggregatorLoop runs the monitor aggregation loop.
func StartAggregatorLoop(ctx context.Context, service *Service) {
	if service != nil {
		service.StartAggregatorLoop(ctx)
	}
}

// StartAggregatorLoop runs the monitor aggregation loop.
func (s *Service) StartAggregatorLoop(ctx context.Context) {
	if s == nil {
		return
	}
	s.superviseLoop(ctx, "monitor_aggregator", &s.aggregatorPanics, func() {
		s.runAggregatorLoop(ctx)
	})
}

func (s *Service) runAggregatorLoop(ctx context.Context) {
	s.loadRecoverySnapshot(ctx)

	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	pending := make([]QueuedEvent, 0, s.flushBatchSize)
	pendingRequests := make([]QueuedRequestEvent, 0, s.flushBatchSize)
	flush := func(flushCtx context.Context) {
		if len(pending) == 0 {
			return
		}
		batch := pending
		pending = make([]QueuedEvent, 0, s.flushBatchSize)
		if err := s.flushBatch(flushCtx, batch); err != nil {
			slog.Warn("monitor_aggregator_flush_failed", "events", len(batch), "error", err)
		}
	}
	flushRequests := func(flushCtx context.Context) {
		if len(pendingRequests) == 0 {
			return
		}
		batch := pendingRequests
		pendingRequests = make([]QueuedRequestEvent, 0, s.flushBatchSize)
		if err := s.flushRequestBatch(flushCtx, batch); err != nil {
			slog.Warn("monitor_request_aggregator_flush_failed", "events", len(batch), "error", err)
		}
	}
	flushAll := func(flushCtx context.Context) {
		flush(flushCtx)
		flushRequests(flushCtx)
	}

	for {
		select {
		case <-ctx.Done():
			tailCtx, cancel := context.WithTimeout(context.Background(), tailFlushTimeout)
			flushAll(tailCtx)
			cancel()
			return
		case op := <-s.queue:
			switch op.Kind {
			case queuedOperationRecord:
				if op.Event.Hash == "" {
					continue
				}
				pending = append(pending, op.Event)
				if len(pending) >= s.flushBatchSize {
					flush(ctx)
				}
			case queuedOperationRecordRequest:
				if op.RequestEvent.Hash == "" {
					continue
				}
				pendingRequests = append(pendingRequests, op.RequestEvent)
				if len(pendingRequests) >= s.flushBatchSize {
					flushRequests(ctx)
				}
			case queuedOperationResolve:
				flushAll(ctx)
				s.resolveBySubject(ctx, op.Resolve)
			default:
				continue
			}
		case <-ticker.C:
			flushAll(ctx)
		}
	}
}

func (s *Service) flushBatch(ctx context.Context, batch []QueuedEvent) error {
	if len(batch) == 0 {
		return nil
	}
	if s.repo == nil {
		return nil
	}
	if err := s.repo.InsertBatch(ctx, batch); err != nil {
		return err
	}
	s.persistRecoveryEvents(ctx, batch)
	s.flushedEvents.Add(int64(len(batch)))
	s.publishMonitorChanged("recorded")
	return nil
}

func (s *Service) flushRequestBatch(ctx context.Context, batch []QueuedRequestEvent) error {
	if len(batch) == 0 {
		return nil
	}
	if s.repo == nil {
		return nil
	}
	if err := s.repo.InsertRequestBatch(ctx, batch); err != nil {
		return err
	}
	s.flushedEvents.Add(int64(len(batch)))
	s.publishMonitorChanged("request_recorded")
	return nil
}

func (s *Service) resolveBySubject(ctx context.Context, query monitoring.ResolveQuery) {
	if s == nil || s.repo == nil {
		return
	}
	if err := s.repo.ResolveBySubject(ctx, query); err != nil {
		slog.Warn("monitor_resolve_by_subject_failed", "error", err)
		return
	}
	s.forgetRecoveryQuery(ctx, query)
	s.publishMonitorChanged("resolved")
}

func (s *Service) superviseLoop(ctx context.Context, name string, counter *atomic.Int64, run func()) {
	for {
		panicked := false
		var panicValue interface{}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					panicked = true
					panicValue = recovered
				}
			}()
			run()
		}()
		if !panicked || ctx.Err() != nil {
			return
		}
		total := counter.Add(1)
		slog.Error("monitor_loop_panic", "loop", name, "panic", panicValue, "restart_count", total)
		s.Record(context.Background(), monitoring.EventInput{
			Type:        monitoring.TypeSystemError,
			Severity:    monitoring.SeverityWarning,
			Source:      monitoring.SourceMonitorWorker,
			SubjectType: monitoring.SubjectSystem,
			SubjectID:   name,
			Title:       "Monitor loop panic",
			Message:     name + " panic recovered",
			ErrorCode:   "monitor_loop_panic",
			Detail: map[string]interface{}{
				"loop":          name,
				"restart_count": total,
				"panic":         panicValue,
			},
		})
		timer := time.NewTimer(loopRestartDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
