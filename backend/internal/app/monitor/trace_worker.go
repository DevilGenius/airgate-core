package monitor

import (
	"context"
	"log/slog"
)

// StartRequestTraceLoop runs the dedicated raw request trace encoder/persister.
func StartRequestTraceLoop(ctx context.Context, service *Service) {
	if service != nil {
		service.StartRequestTraceLoop(ctx)
	}
}

// StartRequestTraceLoop runs only when WithRequestTrace enabled the bounded queue.
func (s *Service) StartRequestTraceLoop(ctx context.Context) {
	if s == nil || s.traceQueue == nil {
		return
	}
	s.superviseLoop(ctx, "monitor_request_trace", &s.workerPanics, func() {
		s.runRequestTraceLoop(ctx)
	})
}

func (s *Service) runRequestTraceLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			tailCtx, cancel := context.WithTimeout(context.Background(), tailFlushTimeout)
			s.flushRequestTraceTail(tailCtx)
			cancel()
			return
		case item := <-s.traceQueue:
			s.persistRequestTrace(ctx, item)
		}
	}
}

func (s *Service) flushRequestTraceTail(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-s.traceQueue:
			s.persistRequestTrace(ctx, item)
		default:
			return
		}
	}
}

func (s *Service) persistRequestTrace(ctx context.Context, item queuedRequestTrace) {
	if item.Bytes > 0 {
		defer s.traceQueuedBytes.Add(-item.Bytes)
	}
	repo, ok := s.repo.(RequestTraceRepository)
	if !ok {
		item.Event.Detail["trace_dropped"] = "repository_unsupported"
		s.enqueueRequestEvent(item.Event)
		return
	}
	stored, err := encodeRequestTrace(item.Trace, s.retention)
	if err != nil {
		slog.Warn("monitor_request_trace_encode_failed", "error", err)
		item.Event.Detail["trace_dropped"] = "encode_failed"
		if flushErr := s.flushRequestBatch(ctx, []QueuedRequestEvent{item.Event}); flushErr != nil {
			slog.Warn("monitor_request_trace_event_fallback_failed", "error", flushErr)
		}
		return
	}
	item.Event.TraceHash = stored.Hash
	if err := repo.UpsertRequestTrace(ctx, stored, item.Event); err != nil {
		slog.Warn("monitor_request_trace_persist_failed", "trace_hash", stored.Hash, "error", err)
		item.Event.TraceHash = ""
		item.Event.Detail["trace_dropped"] = "persist_failed"
		if flushErr := s.flushRequestBatch(ctx, []QueuedRequestEvent{item.Event}); flushErr != nil {
			slog.Warn("monitor_request_trace_event_fallback_failed", "error", flushErr)
		}
		return
	}
	s.flushedEvents.Add(1)
	s.publishMonitorChanged("request_trace_recorded")
}
