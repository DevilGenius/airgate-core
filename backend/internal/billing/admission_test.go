package billing

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSynchronousBillingConcurrencyIsBounded(t *testing.T) {
	r := NewRecorder(nil, 1)
	var active, maximum atomic.Int64
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			release, err := r.acquireSyncWrite(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			n := active.Add(1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
		})
	}
	workers.Wait()
	if maximum.Load() > 4 || len(r.syncWaiters) != 0 {
		t.Fatal("synchronous writers escaped concurrency budget")
	}
}

func TestFullSyncWaitQueueStillRetainsAcceptedEvent(t *testing.T) {
	r := NewRecorder(nil, 1)
	if err := r.EnableJournal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for range cap(r.syncWaiters) {
		r.syncWaiters <- struct{}{}
	}
	_, err := r.RecordSync(t.Context(), UsageRecord{BillingEventID: "queued-during-overload", Platform: "openai", Model: "gpt"})
	if !errors.Is(err, ErrBillingBusy) {
		t.Fatalf("overload = %v", err)
	}
	batch, err := r.journal.Batch(1)
	if err != nil || len(batch) != 1 || batch[0].BillingEventID != "queued-during-overload" {
		t.Fatal("accepted event was lost on sync overload")
	}
}

func TestBillingAdmissionRejectsHighWaterAndRecovers(t *testing.T) {
	r := NewRecorder(nil, 1)
	if err := r.EnableJournal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	r.journal.bytes = journalHighWaterBytes
	r.journal.publishStatsLocked()
	if _, err := r.Reserve(); !errors.Is(err, ErrBillingBusy) {
		t.Fatal("high-water admitted more consumption")
	}
	r.journal.bytes = 0
	r.journal.publishStatsLocked()
	release, err := r.Reserve()
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	if r.producers != 0 {
		t.Fatal("admission reservation leaked")
	}
}
