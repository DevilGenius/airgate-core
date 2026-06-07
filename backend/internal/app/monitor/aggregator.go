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

	pending := make(map[string]AggregatedEvent)
	pendingEvents := 0
	flush := func(flushCtx context.Context) {
		if len(pending) == 0 {
			return
		}
		batch := make([]AggregatedEvent, 0, len(pending))
		for _, event := range pending {
			batch = append(batch, event)
		}
		if err := s.flushBatch(flushCtx, batch); err != nil {
			slog.Warn("monitor_aggregator_flush_failed", "events", pendingEvents, "fingerprints", len(batch), "error", err)
		}
		pending = make(map[string]AggregatedEvent)
		pendingEvents = 0
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
			if existing, ok := pending[event.Fingerprint]; ok {
				pending[event.Fingerprint] = s.mergeAggregated(existing, event)
			} else {
				pending[event.Fingerprint] = event
			}
			pendingEvents++
			if pendingEvents >= s.flushBatchSize {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		}
	}
}

func (s *Service) flushBatch(ctx context.Context, batch []AggregatedEvent) error {
	if len(batch) == 0 {
		return nil
	}
	if s.repo == nil {
		return nil
	}
	if err := s.repo.UpsertBatch(ctx, batch); err != nil {
		return err
	}
	s.flushedEvents.Add(int64(len(batch)))
	return nil
}

func (s *Service) mergeAggregated(existing, incoming AggregatedEvent) AggregatedEvent {
	if incoming.CreatedAt.Before(existing.CreatedAt) {
		existing.CreatedAt = incoming.CreatedAt
	}
	countDelta := existing.CountDelta + incoming.CountDelta
	latest := existing
	if !incoming.UpdatedAt.Before(existing.UpdatedAt) {
		latest = incoming
		latest.CreatedAt = existing.CreatedAt
	}
	latest.CountDelta = countDelta
	latest.Count = countDelta
	if incoming.UpdatedAt.After(existing.UpdatedAt) {
		latest.UpdatedAt = incoming.UpdatedAt
	} else {
		latest.UpdatedAt = existing.UpdatedAt
	}
	latest.Severity = higherSeverity(existing.Severity, incoming.Severity)
	latest.AutoResolveAt = chooseAutoResolveAt(existing, incoming, latest.UpdatedAt)
	latest.ExpiresAt = latest.UpdatedAt.Add(s.retention)
	latest.Detail = mergeDetail(existing.Detail, incoming.Detail, !incoming.UpdatedAt.Before(existing.UpdatedAt))
	return latest
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
			Kind:        monitoring.KindSystemError,
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

func chooseAutoResolveAt(existing, incoming AggregatedEvent, latestUpdatedAt time.Time) *time.Time {
	if !incoming.UpdatedAt.Before(existing.UpdatedAt) {
		return cloneTimePtr(incoming.AutoResolveAt)
	}
	if timePtrAfter(incoming.AutoResolveAt, existing.AutoResolveAt) {
		return cloneTimePtr(incoming.AutoResolveAt)
	}
	if existing.AutoResolveAt == nil {
		t := latestUpdatedAt.Add(autoResolveWindow(existing.Kind))
		return &t
	}
	return cloneTimePtr(existing.AutoResolveAt)
}

func mergeDetail(existing, incoming map[string]interface{}, incomingIsLatest bool) map[string]interface{} {
	if len(existing) == 0 {
		return copyDetail(incoming)
	}
	if len(incoming) == 0 {
		return copyDetail(existing)
	}
	base := existing
	overlay := incoming
	if !incomingIsLatest {
		base = incoming
		overlay = existing
	}
	out := copyDetail(base)
	for key, value := range overlay {
		if previous, ok := out[key]; ok {
			out[key] = mergeDetailValue(previous, value)
			continue
		}
		out[key] = value
	}
	return sanitizeDetail(out)
}

func mergeDetailValue(previous, next interface{}) interface{} {
	prevSlice, prevOK := previous.([]interface{})
	nextSlice, nextOK := next.([]interface{})
	if prevOK && nextOK {
		return mergeDetailSlices(prevSlice, nextSlice)
	}
	return next
}

func mergeDetailSlices(previous, next []interface{}) []interface{} {
	combined := make([]interface{}, 0, len(previous)+len(next))
	combined = append(combined, previous...)
	combined = append(combined, next...)
	if len(combined) <= maxDetailArrayItems {
		return combined
	}
	return combined[len(combined)-maxDetailArrayItems:]
}

func copyDetail(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	t := *value
	return &t
}

func timePtrAfter(left, right *time.Time) bool {
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}
	return left.After(*right)
}

func higherSeverity(left, right string) string {
	if severityRank(right) > severityRank(left) {
		return right
	}
	return left
}

func severityRank(severity string) int {
	switch severity {
	case monitoring.SeverityCritical:
		return 3
	case monitoring.SeverityError:
		return 2
	case monitoring.SeverityWarning:
		return 1
	default:
		return 0
	}
}
