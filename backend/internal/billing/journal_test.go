package billing

import (
	"context"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalCrashHelper(t *testing.T) {
	dir := os.Getenv("AIRGATE_JOURNAL_CRASH_DIR")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	j, err := OpenJournal(dir)
	if err != nil {
		os.Exit(10)
	}
	_, err = j.Append(UsageRecord{BillingEventID: "crash-confirmed", Platform: "openai", Model: "gpt", ActualCost: 1.25, OccurredAt: time.Unix(123, 0), UsageMetadata: map[string]string{"detail": "retained"}})
	if err != nil {
		os.Exit(11)
	}
	os.Exit(23) // Abrupt process exit after the durable acknowledgement.
}

func TestJournalOnlySignalsFullBatchesDuringNormalIntake(t *testing.T) {
	r := NewRecorder(nil, 1)
	if err := r.EnableJournal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for i := range batchSize - 1 {
		if err := r.Record(UsageRecord{BillingEventID: fmt.Sprint(i), Platform: "openai", Model: "gpt"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.journalWake) != 0 {
		t.Fatal("partial batch caused immediate database work")
	}
	if err := r.Record(UsageRecord{BillingEventID: "batch-full", Platform: "openai", Model: "gpt"}); err != nil {
		t.Fatal(err)
	}
	if len(r.journalWake) != 1 {
		t.Fatal("full batch did not wake the writer")
	}
}

func TestJournalRecoversConfirmedEventAfterProcessExit(t *testing.T) {
	dir := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestJournalCrashHelper$")
	command.Env = append(os.Environ(), "AIRGATE_JOURNAL_CRASH_DIR="+dir)
	var exit *exec.ExitError
	if err := command.Run(); !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("crash helper = %v", err)
	}
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := j.Batch(100)
	if err != nil || len(batch) != 1 {
		t.Fatalf("recovery = %d %v", len(batch), err)
	}
	if batch[0].ActualCost != 1.25 || batch[0].UsageMetadata["detail"] != "retained" || !batch[0].OccurredAt.Equal(time.Unix(123, 0)) {
		t.Fatal("recovery changed the financial event")
	}
	duplicate := batch[0]
	duplicate.ActualCost = 999
	stored, err := j.Append(duplicate)
	if err != nil || stored.ActualCost != 1.25 {
		t.Fatal("duplicate replaced accepted event")
	}
}

func TestJournalReplayAfterCommitDoesNotChargeTwice(t *testing.T) {
	db := openBillingRecorderDB(t, "journal_idempotency")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, account, key := createBillingFixture(t, ctx, db, "journal")
	db.User.UpdateOneID(user.ID).SetBalance(10).ExecX(ctx)
	r := NewRecorder(db, 1)
	if err := r.EnableJournal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	record := UsageRecord{BillingEventID: "settled-before-ack", UserID: user.ID, GroupID: group.ID, AccountID: account.ID, APIKeyID: key.ID, Platform: "openai", Model: "gpt", ActualCost: 2, BilledCost: 3, OccurredAt: time.Now()}
	if err := r.Record(record); err != nil {
		t.Fatal(err)
	}
	// Model a crash after database COMMIT but before deleting the event file.
	if _, err := r.recordSyncDatabase(ctx, record); err != nil {
		t.Fatal(err)
	}
	batch, err := r.journal.Batch(100)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.flushJournalBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if got := db.User.GetX(ctx, user.ID).Balance; got != 8 {
		t.Fatalf("balance=%v", got)
	}
	if got := db.APIKey.GetX(ctx, key.ID).UsedQuota; got != 3 {
		t.Fatalf("quota=%v", got)
	}
	if count, _, _ := r.journal.Stats(); count != 0 {
		t.Fatal("settled event was not acknowledged")
	}
}

func TestJournalIsolatesPermanentFailureAndCanReplayAfterRepair(t *testing.T) {
	db := openBillingRecorderDB(t, "journal_bad_record")
	defer closeBillingDB(t, db)
	ctx := t.Context()
	user, group, account, key := createBillingFixture(t, ctx, db, "journal-bad")
	db.User.UpdateOneID(user.ID).SetBalance(10).ExecX(ctx)
	r := NewRecorder(db, 1)
	dir := t.TempDir()
	if err := r.EnableJournal(dir); err != nil {
		t.Fatal(err)
	}
	good := UsageRecord{BillingEventID: "valid", UserID: user.ID, GroupID: group.ID, AccountID: account.ID, APIKeyID: key.ID, Platform: "openai", Model: "gpt", ActualCost: 1, OccurredAt: time.Now()}
	bad := good
	bad.BillingEventID = "invalid-foreign-key"
	bad.AccountID = 900001
	bad.ActualCost = 2
	if err := r.Record(good); err != nil {
		t.Fatal(err)
	}
	if err := r.Record(bad); err != nil {
		t.Fatal(err)
	}
	batch, err := r.journal.Batch(100)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.flushJournalBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if db.User.GetX(ctx, user.ID).Balance != 9 || r.deadLetterTotal.Load() != 1 {
		t.Fatal("bad event blocked good event")
	}
	retained, err := readJournalRecord(filepath.Join(dir, "dead-letter", journalName(bad.BillingEventID)))
	if err != nil || retained.ActualCost != 2 {
		t.Fatal("dead letter did not retain full event")
	}
	restored := db.Account.Create().SetName("restored").SetPlatform("openai").SaveX(ctx)
	var changed entsql.Result
	if err := db.Driver().Exec(ctx, "UPDATE accounts SET id=? WHERE id=?", []any{900001, restored.ID}, &changed); err != nil {
		t.Fatal(err)
	}
	if err := r.journal.Requeue(bad.BillingEventID); err != nil {
		t.Fatal(err)
	}
	batch, err = r.journal.Batch(100)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.flushJournalBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if db.User.GetX(ctx, user.ID).Balance != 7 {
		t.Fatal("repaired event did not settle exactly once")
	}
}
