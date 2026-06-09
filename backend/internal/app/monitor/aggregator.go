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
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	pending := make([]QueuedEvent, 0, s.flushBatchSize)
	flush := func(flushCtx context.Context) {
		if len(pending) == 0 {
			return
		}
		batch := pending
		if err := s.flushBatch(flushCtx, batch); err != nil {
			slog.Warn("monitor_aggregator_flush_failed", "events", len(batch), "error", err)
		}
		pending = make([]QueuedEvent, 0, s.flushBatchSize)
	}

	for {
		select {
		case <-ctx.Done():
			tailCtx, cancel := context.WithTimeout(context.Background(), tailFlushTimeout)
			flush(tailCtx)
			cancel()
			return
		case event := <-s.queue:
			if event.Fingerprint == "" {
				continue
			}
			pending = append(pending, event)
			if len(pending) >= s.flushBatchSize {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
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
	s.flushedEvents.Add(int64(len(batch)))
	return nil
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
			Severity:    monitoring.SeverityError,
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
