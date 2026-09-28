package monitor

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

type traceLifecycleRepo struct {
	monitorRepoStub
	stored            StoredRequestTrace
	traceCleanupCalls int
}

func (r *traceLifecycleRepo) UpsertRequestTrace(_ context.Context, v StoredRequestTrace, _ QueuedRequestEvent) error {
	r.stored = v
	return nil
}
func (r *traceLifecycleRepo) GetRequestTrace(context.Context, string) (StoredRequestTrace, error) {
	return r.stored, nil
}
func (*traceLifecycleRepo) ClearRequestTraces(context.Context, *time.Time) (int, error) {
	return 0, nil
}

// Tripwire: the production trace repository no longer exposes automatic cleanup.
func (r *traceLifecycleRepo) CleanupExpiredRequestTraces(context.Context, time.Time, int) (int, error) {
	r.traceCleanupCalls++
	return 0, nil
}

func TestTraceQueueLifetimeWithoutExpiry(t *testing.T) {
	repo := &traceLifecycleRepo{}
	s := NewService(repo, WithRequestTrace(true), WithRetention(time.Nanosecond))
	now := time.Now().Add(-30 * 24 * time.Hour)
	trace := requestmonitoring.TraceInput{ObservedAt: now, RequestBody: []byte(`{"input":"original"}`), RequestHeaders: http.Header{"Content-Type": {"application/json"}}}
	if !s.RecordRequestTrace(t.Context(), requestmonitoring.EventInput{}, trace) {
		t.Fatal("trace was not queued")
	}
	if s.traceQueuedBytes.Load() != int64(len(trace.RequestBody)) {
		t.Fatal("queued raw body was not accounted")
	}
	item := <-s.traceQueue
	s.persistRequestTrace(t.Context(), item)
	if s.traceQueuedBytes.Load() != 0 {
		t.Fatal("worker retained reserved bytes after persistence")
	}
	if !repo.stored.LastSeenAt.Equal(now) {
		t.Fatal("trace observation time changed")
	}
	if len(repo.stored.Payload) == 0 {
		t.Fatal("trace not persisted")
	}
	s.runCleanupExpiredOnce(t.Context())
	if repo.traceCleanupCalls != 0 || len(repo.stored.Payload) == 0 {
		t.Fatal("event retention deleted permanent trace")
	}
	if repo.cleanupCalls == 0 || repo.cleanupReqCalls == 0 {
		t.Fatal("unrelated monitor retention was disabled")
	}
}
