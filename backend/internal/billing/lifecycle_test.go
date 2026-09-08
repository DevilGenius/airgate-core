package billing

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestConcurrentRecordAndStopDoNotSendToClosedChannel(t *testing.T) {
	db := openBillingRecorderDB(t, "record_stop_race")
	defer closeBillingDB(t, db)
	u, g, a, _ := createBillingFixture(t, t.Context(), db, "record-stop")
	r := NewRecorder(db, 1000)
	r.Start()
	var accepted atomic.Int64
	var writers sync.WaitGroup
	start := make(chan struct{})
	for i := range 64 {
		writers.Go(func() {
			<-start
			record := billingRecordForFixture(fmt.Sprintf("race-%d", i), u, g, a, nil)
			err := r.Record(record)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, errRecorderStopping) {
				t.Error(err)
			}
		})
	}
	close(start)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := r.StopContext(ctx); err != nil {
		t.Fatal(err)
	}
	writers.Wait()
	if count := db.UsageLog.Query().CountX(t.Context()); int64(count) != accepted.Load() {
		t.Fatalf("accepted=%d stored=%d", accepted.Load(), count)
	}
	if err := r.Record(billingRecordForFixture("late", u, g, a, nil)); !errors.Is(err, errRecorderStopping) {
		t.Fatal("stopped recorder accepted a new send")
	}
}

func TestShutdownWaitsForReservedProducerToPublishUsage(t *testing.T) {
	db := openBillingRecorderDB(t, "journal_producer_drain")
	defer closeBillingDB(t, db)
	u, g, a, _ := createBillingFixture(t, t.Context(), db, "producer-drain")
	r := NewRecorder(db, 1)
	if err := r.EnableJournal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	r.Start()
	release, err := r.Reserve()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.StopContext(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("shutdown passed an active producer: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := r.Record(billingRecordForFixture("last-usage", u, g, a, nil)); err != nil {
		t.Fatal(err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if db.UsageLog.Query().CountX(t.Context()) != 1 {
		t.Fatal("final usage was not drained")
	}
}

func TestShutdownDeadlineRetainsJournalAndCancelsDatabaseWrite(t *testing.T) {
	// Canceling a CGO SQLite transaction may discard the last connection.
	db := testdb.OpenEnt(t, "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "billing.db"))+"?_fk=1", schema.WithGlobalUniqueID(false))
	defer closeBillingDB(t, db)
	u, g, a, _ := createBillingFixture(t, t.Context(), db, "deadline")
	dir := t.TempDir()
	r := NewRecorder(db, 1)
	if err := r.EnableJournal(dir); err != nil {
		t.Fatal(err)
	}
	original := recorderQueryUsageInsert
	defer func() { recorderQueryUsageInsert = original }()
	entered := make(chan struct{})
	recorderQueryUsageInsert = func(ctx context.Context, _ *ent.Tx, _ string, _ []any) (usageLogRows, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	r.Start()
	if err := r.Record(billingRecordForFixture("interrupted", u, g, a, nil)); err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := r.StopContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown deadline = %v", err)
	}
	select {
	case <-r.journalDone:
	case <-time.After(time.Second):
		t.Fatal("journal worker did not cancel")
	}
	recorderQueryUsageInsert = original
	if count, _, _ := r.journal.Stats(); count != 1 {
		t.Fatal("pending event disappeared during shutdown")
	}
	if err := r.Record(billingRecordForFixture("late-final-fact", u, g, a, nil)); !errors.Is(err, errRecorderStopping) {
		t.Fatal("late record did not report stopping")
	}
	restarted := NewRecorder(db, 1)
	if err := restarted.EnableJournal(dir); err != nil {
		t.Fatal(err)
	}
	restarted.Start()
	drain, cancelDrain := context.WithTimeout(t.Context(), time.Second)
	defer cancelDrain()
	if err := restarted.StopContext(drain); err != nil {
		t.Fatal(err)
	}
	if db.UsageLog.Query().CountX(t.Context()) != 2 {
		t.Fatal("restart did not settle retained events")
	}
}
