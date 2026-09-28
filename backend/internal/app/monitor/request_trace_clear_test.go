package monitor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type clearTraceRepo struct {
	traceLifecycleRepo
	clear func(context.Context, *time.Time) (int, error)
}

func (r *clearTraceRepo) ClearRequestTraces(ctx context.Context, before *time.Time) (int, error) {
	return r.clear(ctx, before)
}

func TestClearRequestEventsIncludesTraceCleanup(t *testing.T) {
	cutoff, zero := time.Unix(1700000000, 0), time.Time{}
	for _, before := range []*time.Time{nil, &zero, &cutoff} {
		var order []string
		repo := &clearTraceRepo{clear: func(_ context.Context, got *time.Time) (int, error) {
			if got != before {
				t.Fatal("trace cutoff changed")
			}
			order = append(order, "traces")
			return 9, nil
		}}
		repo.clearRequestEvents = func(_ context.Context, got *time.Time) (int, error) {
			if got != before {
				t.Fatal("event cutoff changed")
			}
			order = append(order, "events")
			return 3, nil
		}
		deleted, err := NewService(repo).ClearRequestEvents(t.Context(), before)
		if err != nil || deleted != 3 || !reflect.DeepEqual(order, []string{"events", "traces"}) {
			t.Fatalf("clear contract changed: deleted=%d order=%v err=%v", deleted, order, err)
		}
	}
}

func TestClearRequestEventsReportsPartialFailure(t *testing.T) {
	want := errors.New("trace cleanup failed")
	repo := &clearTraceRepo{clear: func(context.Context, *time.Time) (int, error) { return 0, want }}
	repo.clearRequestEvents = func(context.Context, *time.Time) (int, error) { return 3, nil }
	deleted, err := NewService(repo).ClearRequestEvents(t.Context(), nil)
	if deleted != 3 || !errors.Is(err, want) {
		t.Fatalf("partial result lost: %d / %v", deleted, err)
	}
	repo.clearRequestEvents = func(context.Context, *time.Time) (int, error) { return 0, want }
	repo.clear = func(context.Context, *time.Time) (int, error) {
		t.Fatal("trace cleanup followed failed event deletion")
		return 0, nil
	}
	if _, err := NewService(repo).ClearRequestEvents(t.Context(), nil); !errors.Is(err, want) {
		t.Fatal(err)
	}
}
