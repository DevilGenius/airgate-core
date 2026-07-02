package billing

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/go-redis/redismock/v9"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/usagelog"
	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestRecordSyncPersistsUserEmailSnapshot(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "billing_recorder", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-snapshot@example.com")
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("acc").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	recorder := NewRecorder(db, 0)
	usageID, err := recorder.RecordSync(ctx, UsageRecord{
		UserID:    user.ID,
		UserEmail: user.Email,
		AccountID: account.ID,
		GroupID:   group.ID,
		Platform:  "openai",
		Model:     "gpt-5",
	})
	if err != nil {
		t.Fatalf("RecordSync returned error: %v", err)
	}

	log, err := db.UsageLog.Get(ctx, usageID)
	if err != nil {
		t.Fatalf("get usage log: %v", err)
	}
	if log.UserIDSnapshot != user.ID || log.UserEmailSnapshot != user.Email {
		t.Fatalf("usage snapshot = (%d, %q), want (%d, %q)", log.UserIDSnapshot, log.UserEmailSnapshot, user.ID, user.Email)
	}
	if log.BillingEventID == "" {
		t.Fatalf("billing_event_id should be generated")
	}
	if log.RateMultiplier != 1 || log.AccountRateMultiplier != 1 {
		t.Fatalf("rate snapshots = (%v, %v), want defaults (1, 1)", log.RateMultiplier, log.AccountRateMultiplier)
	}
}

func TestBatchInsertIsIdempotentByBillingEventID(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "billing_recorder_idempotent", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-idempotent@example.com")
	if err := db.User.UpdateOneID(user.ID).SetBalance(10).Exec(ctx); err != nil {
		t.Fatalf("set user balance: %v", err)
	}
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("acc").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("key").
		SetKeyHash("hash-idempotent").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	recorder := NewRecorder(db, 0)
	record := UsageRecord{
		BillingEventID: "bill_test_idempotent",
		UserID:         user.ID,
		UserEmail:      user.Email,
		APIKeyID:       key.ID,
		AccountID:      account.ID,
		GroupID:        group.ID,
		Platform:       "openai",
		Model:          "gpt-5",
		ActualCost:     1.25,
		BilledCost:     2.50,
		AccountCost:    0.75,
	}
	if err := recorder.batchInsert(ctx, []UsageRecord{record}); err != nil {
		t.Fatalf("first batchInsert: %v", err)
	}
	if err := recorder.batchInsert(ctx, []UsageRecord{record}); err != nil {
		t.Fatalf("duplicate batchInsert: %v", err)
	}

	count, err := db.UsageLog.Query().
		Where(usagelog.BillingEventIDEQ(record.BillingEventID)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count usage logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("usage log count = %d, want 1", count)
	}
	userAfter, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if userAfter.Balance != 8.75 {
		t.Fatalf("user balance = %.2f, want 8.75", userAfter.Balance)
	}
	keyAfter, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get api key: %v", err)
	}
	if keyAfter.UsedQuota != 2.50 || keyAfter.UsedQuotaActual != 1.25 {
		t.Fatalf("api key usage = (%.2f, %.2f), want (2.50, 1.25)", keyAfter.UsedQuota, keyAfter.UsedQuotaActual)
	}
}

func TestRecordSyncRejectsInsufficientBalanceAndQuotaAtomically(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "billing_atomic_quota", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user, group, account, key := createBillingFixture(t, ctx, db, "atomic")
	if err := db.User.UpdateOneID(user.ID).SetBalance(1).Exec(ctx); err != nil {
		t.Fatalf("set user balance: %v", err)
	}
	recorder := NewRecorder(db, 0)

	overBalance := billingRecordForFixture("bill_atomic_balance", user, group, account, key)
	overBalance.ActualCost = 2
	if _, err := recorder.RecordSync(ctx, overBalance); err == nil || !strings.Contains(err.Error(), "用户余额不足") {
		t.Fatalf("over balance RecordSync error = %v", err)
	}
	if exists, err := db.UsageLog.Query().Where(usagelog.BillingEventIDEQ(overBalance.BillingEventID)).Exist(ctx); err != nil || exists {
		t.Fatalf("over balance usage exists=%v err=%v", exists, err)
	}
	userAfter, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user after balance reject: %v", err)
	}
	if userAfter.Balance != 1 {
		t.Fatalf("balance after reject = %.2f, want 1", userAfter.Balance)
	}

	if err := db.User.UpdateOneID(user.ID).SetBalance(100).Exec(ctx); err != nil {
		t.Fatalf("restore user balance: %v", err)
	}
	if err := db.APIKey.UpdateOneID(key.ID).SetQuotaUsd(5).SetUsedQuota(4.5).Exec(ctx); err != nil {
		t.Fatalf("set key quota: %v", err)
	}
	overQuota := billingRecordForFixture("bill_atomic_quota", user, group, account, key)
	overQuota.ActualCost = 0.25
	overQuota.BilledCost = 1
	if _, err := recorder.RecordSync(ctx, overQuota); err == nil || !strings.Contains(err.Error(), "API Key 额度不足") {
		t.Fatalf("over quota RecordSync error = %v", err)
	}
	if exists, err := db.UsageLog.Query().Where(usagelog.BillingEventIDEQ(overQuota.BillingEventID)).Exist(ctx); err != nil || exists {
		t.Fatalf("over quota usage exists=%v err=%v", exists, err)
	}
	keyAfter, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get key after quota reject: %v", err)
	}
	if keyAfter.UsedQuota != 4.5 || keyAfter.UsedQuotaActual != 0 {
		t.Fatalf("key usage after reject = (%.2f, %.2f), want (4.50, 0)", keyAfter.UsedQuota, keyAfter.UsedQuotaActual)
	}

	atLimit := billingRecordForFixture("bill_atomic_quota_exact", user, group, account, key)
	atLimit.ActualCost = 0.25
	atLimit.BilledCost = 0.5
	if _, err := recorder.RecordSync(ctx, atLimit); err != nil {
		t.Fatalf("exact quota RecordSync error = %v", err)
	}
	keyAfter, err = db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get key after exact quota: %v", err)
	}
	if keyAfter.UsedQuota != 5 || keyAfter.UsedQuotaActual != 0.25 {
		t.Fatalf("key usage after exact quota = (%.2f, %.2f), want (5.00, 0.25)", keyAfter.UsedQuota, keyAfter.UsedQuotaActual)
	}
}

func TestRecorderStartAndCallbackUseInjectedRunner(t *testing.T) {
	var labels []string
	oldGo := recorderGo
	recorderGo = func(name string, fn func()) {
		labels = append(labels, name)
	}
	defer func() { recorderGo = oldGo }()

	recorder := NewRecorder(nil, -1)
	if cap(recorder.ch) != defaultBufferSize {
		t.Fatalf("buffer cap = %d, want default %d", cap(recorder.ch), defaultBufferSize)
	}
	recorder.Start()
	if !reflect.DeepEqual(labels, []string{"billing_recorder", "billing_recorder_retry"}) {
		t.Fatalf("runner labels = %#v", labels)
	}

	called := false
	recorder.SetAPIKeyBalanceAlertCallback(func(APIKeyBalanceAlertInput) {
		called = true
	})
	cb := recorder.apiKeyBalanceAlertCallback()
	if cb == nil {
		t.Fatalf("callback should be stored")
	}
	cb(APIKeyBalanceAlertInput{})
	if !called {
		t.Fatalf("stored callback was not invoked")
	}
}

func TestRecordQueuesAndFallsBackWhenBufferFull(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_record_fallback")
	defer closeBillingDB(t, db)

	ctx := context.Background()
	user, group, account, _ := createBillingFixture(t, ctx, db, "record-fallback")
	recorder := NewRecorder(db, 1)

	queued := billingRecordForFixture("bill_queue", user, group, account, nil)
	recorder.Record(queued)
	fallback := billingRecordForFixture("bill_fallback", user, group, account, nil)
	recorder.Record(fallback)

	select {
	case got := <-recorder.ch:
		if got.BillingEventID != queued.BillingEventID {
			t.Fatalf("queued event id = %q, want %q", got.BillingEventID, queued.BillingEventID)
		}
	case <-time.After(time.Second):
		t.Fatalf("record was not queued")
	}

	if _, err := db.UsageLog.Query().
		Where(usagelog.BillingEventIDEQ(fallback.BillingEventID)).
		Only(ctx); err != nil {
		t.Fatalf("fallback record was not persisted: %v", err)
	}

	recorder.Record(queued)
	bad := queued
	bad.BillingEventID = "bill_bad_fallback"
	bad.Platform = ""
	recorder.Record(bad)
}

func TestRunFlushesTickerAndStopBatches(t *testing.T) {
	t.Run("ticker", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_run_ticker")
		defer closeBillingDB(t, db)
		ctx := context.Background()
		user, group, account, _ := createBillingFixture(t, ctx, db, "run-ticker")
		recorder := NewRecorder(db, 2)

		oldInterval := recorderFlushInterval
		recorderFlushInterval = time.Millisecond
		defer func() { recorderFlushInterval = oldInterval }()

		go recorder.run()
		recorder.ch <- billingRecordForFixture("bill_ticker", user, group, account, nil)
		waitForUsageLog(t, ctx, db, "bill_ticker")
		close(recorder.stopCh)
		waitClosed(t, recorder.stopped)
	})

	t.Run("stop", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_run_stop")
		defer closeBillingDB(t, db)
		ctx := context.Background()
		user, group, account, _ := createBillingFixture(t, ctx, db, "run-stop")
		recorder := NewRecorder(db, 2)

		oldInterval := recorderFlushInterval
		recorderFlushInterval = time.Hour
		defer func() { recorderFlushInterval = oldInterval }()

		go recorder.run()
		recorder.ch <- billingRecordForFixture("bill_stop", user, group, account, nil)
		close(recorder.stopCh)
		waitClosed(t, recorder.stopped)
		waitForUsageLog(t, ctx, db, "bill_stop")
	})
}

func TestStopFlushesStartedRecorder(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_stop_method")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, account, _ := createBillingFixture(t, ctx, db, "stop-method")
	recorder := NewRecorder(db, 2)

	oldInterval := recorderFlushInterval
	recorderFlushInterval = time.Hour
	defer func() { recorderFlushInterval = oldInterval }()

	recorder.Start()
	recorder.Record(billingRecordForFixture("bill_stop_method", user, group, account, nil))
	recorder.Stop()
	recorder.Stop()
	waitForUsageLog(t, ctx, db, "bill_stop_method")
}

func TestRunFlushesWhenBatchSizeReached(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_run_batch_size")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, account, _ := createBillingFixture(t, ctx, db, "run-batch")
	recorder := NewRecorder(db, batchSize)

	oldInterval := recorderFlushInterval
	recorderFlushInterval = time.Hour
	defer func() { recorderFlushInterval = oldInterval }()

	go recorder.run()
	for i := 0; i < batchSize; i++ {
		recorder.ch <- billingRecordForFixture("bill_batch_"+time.Unix(int64(i), 0).UTC().Format("150405"), user, group, account, nil)
	}
	waitForUsageLog(t, ctx, db, "bill_batch_000139")
	close(recorder.stopCh)
	waitClosed(t, recorder.stopped)
}

func TestFlushRetryAndRetryQueueBranches(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_retry")
	defer closeBillingDB(t, db)

	ctx := context.Background()
	user, group, account, _ := createBillingFixture(t, ctx, db, "retry")
	recorder := NewRecorder(db, 1)
	valid := billingRecordForFixture("bill_retry_success", user, group, account, nil)

	oldBackoff := recorderRetryBackoff
	recorderRetryBackoff = func(int) time.Duration { return 0 }
	defer func() { recorderRetryBackoff = oldBackoff }()

	if err := recorder.flushRetryBatch(ctx, []UsageRecord{valid}); err != nil {
		t.Fatalf("flushRetryBatch valid: %v", err)
	}
	waitForUsageLog(t, ctx, db, valid.BillingEventID)

	invalid := valid
	invalid.BillingEventID = "bill_retry_invalid"
	invalid.Platform = ""
	if err := recorder.flushRetryBatch(ctx, []UsageRecord{invalid}); err == nil {
		t.Fatalf("flushRetryBatch invalid should fail")
	}

	go recorder.runRetries()
	recorder.retryCh <- []UsageRecord{invalid}
	close(recorder.retryCh)
	waitClosed(t, recorder.retryStopped)

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	recorderRetryBackoff = func(int) time.Duration { return time.Hour }
	if err := recorder.flushRetryBatch(cancelCtx, []UsageRecord{valid}); !errors.Is(err, context.Canceled) {
		t.Fatalf("flushRetryBatch canceled error = %v, want context.Canceled", err)
	}

	errAfterFailure := NewRecorder(db, 1)
	retryCtx, retryCancel := context.WithCancel(ctx)
	retryWaits := 0
	recorderRetryBackoff = func(int) time.Duration {
		retryWaits++
		if retryWaits > 1 {
			retryCancel()
			return time.Hour
		}
		return 0
	}
	badThenCanceled := valid
	badThenCanceled.BillingEventID = "bill_retry_last_error"
	badThenCanceled.Platform = ""
	if err := errAfterFailure.flushRetryBatch(retryCtx, []UsageRecord{badThenCanceled}); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("flushRetryBatch should return the last write error after a failed attempt, got %v", err)
	}

	oldMaxAttempts := recorderMaxAttempts
	recorderMaxAttempts = 1
	if err := recorder.flushRetryBatch(ctx, []UsageRecord{valid}); err == nil || !strings.Contains(err.Error(), "retry exhausted") {
		t.Fatalf("flushRetryBatch exhausted error = %v", err)
	}
	recorderMaxAttempts = oldMaxAttempts
}

func TestFlushBranches(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_flush_branches")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, account, _ := createBillingFixture(t, ctx, db, "flush-branches")
	recorder := NewRecorder(db, 1)

	recorder.flush(ctx, nil)
	bad := billingRecordForFixture("bill_flush_bad", user, group, account, nil)
	bad.Platform = ""
	recorder.flush(ctx, []UsageRecord{bad})
	if got := len(recorder.retryCh); got != 1 {
		t.Fatalf("retry queue length after failed flush = %d, want 1", got)
	}
}

func TestEnqueueRetryAndWaitRetryBranches(t *testing.T) {
	record := UsageRecord{BillingEventID: "bill_retry_queue"}
	cause := errors.New("write failed")

	recorder := NewRecorder(nil, 1)
	recorder.enqueueRetry(context.Background(), []UsageRecord{record}, cause)
	if got := len(recorder.retryCh); got != 1 {
		t.Fatalf("retry queue length = %d, want 1", got)
	}

	full := NewRecorder(nil, 1)
	for i := 0; i < cap(full.retryCh); i++ {
		full.retryCh <- []UsageRecord{record}
	}
	full.enqueueRetry(context.Background(), []UsageRecord{record}, cause)

	canceled := NewRecorder(nil, 1)
	for i := 0; i < cap(canceled.retryCh); i++ {
		canceled.retryCh <- []UsageRecord{record}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled.enqueueRetry(ctx, []UsageRecord{record}, cause)

	stopping := NewRecorder(nil, 1)
	for i := 0; i < cap(stopping.retryCh); i++ {
		stopping.retryCh <- []UsageRecord{record}
	}
	close(stopping.stopCh)
	stopping.enqueueRetry(context.Background(), []UsageRecord{record}, cause)

	if err := recorder.waitRetry(context.Background(), 0); err != nil {
		t.Fatalf("waitRetry timer branch: %v", err)
	}
	waitCtx, waitCancel := context.WithCancel(context.Background())
	waitCancel()
	if err := recorder.waitRetry(waitCtx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitRetry canceled = %v, want context.Canceled", err)
	}
	waitStop := NewRecorder(nil, 1)
	close(waitStop.stopCh)
	if err := waitStop.waitRetry(context.Background(), time.Hour); !errors.Is(err, errRecorderStopping) {
		t.Fatalf("waitRetry stop = %v, want errRecorderStopping", err)
	}
}

func TestRecorderHelperBranches(t *testing.T) {
	if nextRetryBackoff(2) != time.Second {
		t.Fatalf("nextRetryBackoff(2) = %v", nextRetryBackoff(2))
	}
	if nextRetryBackoff(999) != maxRetryBackoff {
		t.Fatalf("nextRetryBackoff cap = %v, want %v", nextRetryBackoff(999), maxRetryBackoff)
	}

	ctxWithDeadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	gotCtx, gotCancel := withWriteTimeout(ctxWithDeadline)
	gotCancel()
	if gotCtx != ctxWithDeadline {
		t.Fatalf("withWriteTimeout should keep existing deadline context")
	}
	newCtx, newCancel := withWriteTimeout(context.Background())
	defer newCancel()
	if _, ok := newCtx.Deadline(); !ok {
		t.Fatalf("withWriteTimeout should add a deadline")
	}

	if got := nullablePositiveID(0); got != nil {
		t.Fatalf("nullablePositiveID(0) = %#v, want nil", got)
	}
	if got := nullablePositiveID(7); got != 7 {
		t.Fatalf("nullablePositiveID(7) = %#v, want 7", got)
	}

	records := []UsageRecord{{}, {BillingEventID: "bill_existing"}}
	ensureBatchBillingEventIDs(records)
	if records[0].BillingEventID == "" || !strings.HasPrefix(records[0].BillingEventID, "bill_") {
		t.Fatalf("missing billing event id was not generated: %q", records[0].BillingEventID)
	}
	if records[1].BillingEventID != "bill_existing" {
		t.Fatalf("existing billing event id was changed: %q", records[1].BillingEventID)
	}

	for name, rec := range map[string]UsageRecord{
		"event":    {Platform: "openai", Model: "gpt-5"},
		"platform": {BillingEventID: "bill_missing_platform", Model: "gpt-5"},
		"model":    {BillingEventID: "bill_missing_model", Platform: "openai"},
	} {
		if err := validateUsageRecordForInsert(rec); err == nil {
			t.Fatalf("validateUsageRecordForInsert %s should fail", name)
		}
	}
	if err := validateUsageRecordForInsert(UsageRecord{
		BillingEventID: "bill_valid",
		Platform:       "openai",
		Model:          "gpt-5",
	}); err != nil {
		t.Fatalf("validateUsageRecordForInsert valid: %v", err)
	}

	if value, err := usageMetadataValue(nil); err != nil || value != nil {
		t.Fatalf("empty metadata = (%#v, %v), want nil nil", value, err)
	}
	value, err := usageMetadataValue(map[string]string{"trace": "abc"})
	if err != nil {
		t.Fatalf("metadata marshal: %v", err)
	}
	if value != `{"trace":"abc"}` {
		t.Fatalf("metadata value = %#v", value)
	}

	oldMarshal := marshalUsageMetadata
	defer func() { marshalUsageMetadata = oldMarshal }()
	marshalUsageMetadata = func(any) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	if _, err := usageMetadataValue(map[string]string{"trace": "abc"}); err == nil {
		t.Fatalf("usageMetadataValue should return marshal error")
	}
	if _, err := usageLogInsertValues(UsageRecord{UsageMetadata: map[string]string{"trace": "abc"}}, time.Now()); err == nil {
		t.Fatalf("usageLogInsertValues should return metadata error")
	}
	marshalUsageMetadata = oldMarshal

	values, err := usageLogInsertValues(UsageRecord{
		BillingEventID: "bill_values",
		Platform:       "openai",
		Model:          "gpt-5",
		UserID:         1,
		APIKeyID:       -1,
		AccountID:      2,
		GroupID:        3,
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("usageLogInsertValues valid: %v", err)
	}
	if len(values) != len(usageLogInsertColumns()) {
		t.Fatalf("value count = %d, want columns %d", len(values), len(usageLogInsertColumns()))
	}

	inserted := insertedUsageRecords([]insertedUsageLog{{Record: UsageRecord{BillingEventID: "a"}}, {Record: UsageRecord{BillingEventID: "b"}}})
	if !reflect.DeepEqual(billingEventIDs(inserted), []string{"a", "b"}) {
		t.Fatalf("inserted ids = %#v", billingEventIDs(inserted))
	}
}

func TestRecordSyncDuplicateAndErrorBranches(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_recordsync_duplicate")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, account, _ := createBillingFixture(t, ctx, db, "recordsync-duplicate")
	recorder := NewRecorder(db, 1)
	record := billingRecordForFixture("bill_recordsync_duplicate", user, group, account, nil)

	firstID, err := recorder.RecordSync(ctx, record)
	if err != nil {
		t.Fatalf("RecordSync first: %v", err)
	}
	secondID, err := recorder.RecordSync(ctx, record)
	if err != nil {
		t.Fatalf("RecordSync duplicate: %v", err)
	}
	if secondID != firstID {
		t.Fatalf("duplicate usage id = %d, want %d", secondID, firstID)
	}
	oldFindUsageID := recorderFindUsageID
	recorderFindUsageID = func(context.Context, *ent.Tx, string) (int, error) {
		return 0, errors.New("find failed")
	}
	if _, err := recorder.RecordSync(ctx, record); err == nil || !strings.Contains(err.Error(), "查询已有 UsageLog 失败") {
		t.Fatalf("RecordSync duplicate lookup error = %v", err)
	}
	recorderFindUsageID = oldFindUsageID

	closedDB := openBillingRecorderDB(t, "billing_recordsync_closed")
	if err := closedDB.Close(); err != nil {
		t.Fatalf("close db before RecordSync: %v", err)
	}
	if _, err := NewRecorder(closedDB, 1).RecordSync(ctx, record); err == nil {
		t.Fatalf("RecordSync should fail when transaction cannot start")
	}
}

func TestRecordSyncCommitAndChargeErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("commit after insert", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_recordsync_commit_insert")
		defer closeBillingDB(t, db)
		user, group, account, _ := createBillingFixture(t, ctx, db, "recordsync-commit-insert")
		recorder := NewRecorder(db, 1)
		record := billingRecordForFixture("bill_recordsync_commit_insert", user, group, account, nil)

		oldCommit := recorderCommitTx
		recorderCommitTx = func(*ent.Tx) error { return errors.New("commit failed") }
		defer func() { recorderCommitTx = oldCommit }()
		if _, err := recorder.RecordSync(ctx, record); err == nil || !strings.Contains(err.Error(), "提交事务失败") {
			t.Fatalf("RecordSync commit insert error = %v", err)
		}
	})

	t.Run("commit after duplicate lookup", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_recordsync_commit_duplicate")
		defer closeBillingDB(t, db)
		user, group, account, _ := createBillingFixture(t, ctx, db, "recordsync-commit-duplicate")
		recorder := NewRecorder(db, 1)
		record := billingRecordForFixture("bill_recordsync_commit_duplicate", user, group, account, nil)
		if _, err := recorder.RecordSync(ctx, record); err != nil {
			t.Fatalf("RecordSync seed: %v", err)
		}

		oldCommit := recorderCommitTx
		recorderCommitTx = func(*ent.Tx) error { return errors.New("commit failed") }
		defer func() { recorderCommitTx = oldCommit }()
		if _, err := recorder.RecordSync(ctx, record); err == nil || !strings.Contains(err.Error(), "提交事务失败") {
			t.Fatalf("RecordSync commit duplicate error = %v", err)
		}
	})

	t.Run("charge error", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_recordsync_charge_error")
		defer closeBillingDB(t, db)
		user, group, account, _ := createBillingFixture(t, ctx, db, "recordsync-charge-error")
		db.User.Use(errorUserMutationHook(ent.OpUpdate, errors.New("charge failed")))
		record := billingRecordForFixture("bill_recordsync_charge_error", user, group, account, nil)
		record.ActualCost = 1
		if _, err := NewRecorder(db, 1).RecordSync(ctx, record); err == nil || !strings.Contains(err.Error(), "扣减用户余额失败") {
			t.Fatalf("RecordSync charge error = %v", err)
		}
	})
}

func TestBatchInsertTxCommitAndChargeErrors(t *testing.T) {
	ctx := context.Background()

	closedDB := openBillingRecorderDB(t, "billing_batch_closed")
	if err := closedDB.Close(); err != nil {
		t.Fatalf("close db before batchInsert: %v", err)
	}
	if err := NewRecorder(closedDB, 1).batchInsert(ctx, []UsageRecord{{Platform: "openai", Model: "gpt-5"}}); err == nil {
		t.Fatalf("batchInsert should fail when transaction cannot start")
	}

	t.Run("commit after insert", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_batch_commit_insert")
		defer closeBillingDB(t, db)
		user, group, account, _ := createBillingFixture(t, ctx, db, "batch-commit-insert")
		record := billingRecordForFixture("bill_batch_commit_insert", user, group, account, nil)

		oldCommit := recorderCommitTx
		recorderCommitTx = func(*ent.Tx) error { return errors.New("commit failed") }
		defer func() { recorderCommitTx = oldCommit }()
		if err := NewRecorder(db, 1).batchInsert(ctx, []UsageRecord{record}); err == nil || !strings.Contains(err.Error(), "提交事务失败") {
			t.Fatalf("batchInsert commit insert error = %v", err)
		}
	})

	t.Run("commit after duplicate no-op", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_batch_commit_duplicate")
		defer closeBillingDB(t, db)
		user, group, account, _ := createBillingFixture(t, ctx, db, "batch-commit-duplicate")
		recorder := NewRecorder(db, 1)
		record := billingRecordForFixture("bill_batch_commit_duplicate", user, group, account, nil)
		if err := recorder.batchInsert(ctx, []UsageRecord{record}); err != nil {
			t.Fatalf("batchInsert seed: %v", err)
		}

		oldCommit := recorderCommitTx
		recorderCommitTx = func(*ent.Tx) error { return errors.New("commit failed") }
		defer func() { recorderCommitTx = oldCommit }()
		if err := recorder.batchInsert(ctx, []UsageRecord{record}); err == nil || !strings.Contains(err.Error(), "提交事务失败") {
			t.Fatalf("batchInsert duplicate commit error = %v", err)
		}
	})

	t.Run("charge error", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_batch_charge_error")
		defer closeBillingDB(t, db)
		user, group, account, _ := createBillingFixture(t, ctx, db, "batch-charge-error")
		db.User.Use(errorUserMutationHook(ent.OpUpdate, errors.New("charge failed")))
		record := billingRecordForFixture("bill_batch_charge_error", user, group, account, nil)
		record.ActualCost = 1
		if err := NewRecorder(db, 1).batchInsert(ctx, []UsageRecord{record}); err == nil || !strings.Contains(err.Error(), "扣减用户余额失败") {
			t.Fatalf("batchInsert charge error = %v", err)
		}
	})
}

func TestInsertUsageLogsBranches(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_insert_branches")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, account, _ := createBillingFixture(t, ctx, db, "insert-branches")
	recorder := NewRecorder(db, 1)
	record := billingRecordForFixture("bill_insert_branches", user, group, account, nil)

	tx, err := db.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx: %v", err)
	}
	inserted, err := recorder.insertUsageLogs(ctx, tx, nil)
	if err != nil || inserted != nil {
		t.Fatalf("empty insertUsageLogs = (%#v, %v), want nil nil", inserted, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback empty tx: %v", err)
	}

	tx, err = db.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx for marshal error: %v", err)
	}
	oldMarshal := marshalUsageMetadata
	marshalUsageMetadata = func(any) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	if _, err := recorder.insertUsageLogs(ctx, tx, []UsageRecord{record}); err == nil {
		t.Fatalf("insertUsageLogs should return metadata error")
	}
	marshalUsageMetadata = oldMarshal
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback marshal tx: %v", err)
	}

	tx, err = db.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx for query error: %v", err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := recorder.insertUsageLogs(cancelCtx, tx, []UsageRecord{record}); err == nil {
		t.Fatalf("insertUsageLogs should return canceled query error")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback canceled tx: %v", err)
	}

	tx, err = db.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx for unsupported dialect: %v", err)
	}
	oldDialect := recorderInsertDialect
	recorderInsertDialect = func(*ent.Tx) string { return "mysql" }
	if _, err := recorder.insertUsageLogs(ctx, tx, []UsageRecord{record}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("insertUsageLogs unsupported dialect error = %v", err)
	}
	recorderInsertDialect = oldDialect
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback unsupported tx: %v", err)
	}

	runRowsCase := func(name string, rows usageLogRows, want string) {
		t.Helper()
		tx, err := db.Tx(ctx)
		if err != nil {
			t.Fatalf("start tx for %s: %v", name, err)
		}
		oldQuery := recorderQueryUsageInsert
		recorderQueryUsageInsert = func(context.Context, *ent.Tx, string, []any) (usageLogRows, error) {
			return rows, nil
		}
		_, err = recorder.insertUsageLogs(ctx, tx, []UsageRecord{record})
		recorderQueryUsageInsert = oldQuery
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("insertUsageLogs %s error = %v, want contains %q", name, err, want)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback %s tx: %v", name, err)
		}
	}
	runRowsCase("scan", &fakeUsageLogRows{
		nexts: []bool{true},
		scan: func(dest ...any) error {
			return errors.New("scan failed")
		},
	}, "scan failed")
	runRowsCase("unknown event", &fakeUsageLogRows{
		nexts: []bool{true},
		scan: func(dest ...any) error {
			*(dest[0].(*int)) = 123
			*(dest[1].(*string)) = "bill_unknown"
			return nil
		},
	}, "未知 billing_event_id")
	runRowsCase("rows err", &fakeUsageLogRows{
		err: errors.New("rows failed"),
	}, "rows failed")
}

func TestUpdateAccountStatsCacheWritesRedisCommands(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	recorder := NewRecorder(nil, 1, rdb)
	ctx := context.Background()

	todayPattern := `ag:account:stats:today:[0-9]{8}`
	updatedAtPattern := `^[0-9]{4}-[0-9]{2}-[0-9]{2}T.*Z$`
	imageTodayPattern := `ag:account:image:today:[0-9]{8}:42`

	mock.Regexp().ExpectHIncrBy(todayPattern, "42:requests", 1).SetVal(1)
	mock.Regexp().ExpectHIncrBy(todayPattern, "42:tokens", 15).SetVal(15)
	mock.Regexp().ExpectHIncrByFloat(todayPattern, "42:account_cost", 0.75).SetVal(0.75)
	mock.Regexp().ExpectHIncrByFloat(todayPattern, "42:user_cost", 1.25).SetVal(1.25)
	mock.Regexp().ExpectHSet(todayPattern, "42:updated_at", updatedAtPattern).SetVal(1)
	mock.Regexp().ExpectExpire(todayPattern, accountcache.TodayStatsTTL).SetVal(true)
	mock.ExpectIncr(accountcache.ImageTotalKey(42)).SetVal(1)
	mock.ExpectExpire(accountcache.ImageTotalKey(42), accountcache.ImageTotalTTL).SetVal(true)
	mock.Regexp().ExpectIncr(imageTodayPattern).SetVal(1)
	mock.Regexp().ExpectExpire(imageTodayPattern, accountcache.TodayStatsTTL).SetVal(true)

	recorder.updateAccountStatsCache(ctx, []UsageRecord{
		{
			AccountID:             0,
			InputTokens:           100,
			OutputTokens:          100,
			AccountCost:           99,
			ActualCost:            99,
			Model:                 "gpt-image-ignored",
			BillingEventID:        "ignored",
			APIKeyID:              0,
			AccountRateMultiplier: 1,
		},
		{
			AccountID:           42,
			InputTokens:         10,
			OutputTokens:        2,
			CachedInputTokens:   1,
			CacheCreationTokens: 2,
			AccountCost:         0.75,
			ActualCost:          1.25,
			Model:               "gpt-image-1",
		},
	})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}

	NewRecorder(nil, 1).updateAccountStatsCache(ctx, []UsageRecord{{AccountID: 42}})
	recorder.updateAccountStatsCache(ctx, nil)
}

func TestUpdateAccountStatsCacheLogsRedisExecError(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	recorder := NewRecorder(nil, 1, rdb)
	ctx := context.Background()

	todayPattern := `ag:account:stats:today:[0-9]{8}`
	mock.Regexp().ExpectHIncrBy(todayPattern, "7:requests", 1).SetErr(errors.New("redis down"))

	recorder.updateAccountStatsCache(ctx, []UsageRecord{{AccountID: 7, Model: "gpt-5"}})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestAPIKeyBalanceAlertCandidatesAndChecks(t *testing.T) {
	if got := apiKeyBalanceAlertCandidateIDs([]UsageRecord{
		{APIKeyID: 0, BilledCost: 1},
		{APIKeyID: 1, BilledCost: 0},
		{APIKeyID: 2, BilledCost: 1},
		{APIKeyID: 2, BilledCost: 2},
		{APIKeyID: 3, BilledCost: 1},
	}); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("candidate ids = %#v", got)
	}

	db := openBillingRecorderDB(t, "billing_alerts")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, _, _ := createBillingFixture(t, ctx, db, "alerts")
	alertKey, err := db.APIKey.Create().
		SetName("alert-key").
		SetKeyHash("alert-hash").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetQuotaUsd(5).
		SetUsedQuota(4.5).
		SetBalanceAlertEnabled(true).
		SetBalanceAlertEmail(" alert@example.com ").
		SetBalanceAlertThreshold(1).
		Save(ctx)
	if err != nil {
		t.Fatalf("create alert key: %v", err)
	}
	resetKey, err := db.APIKey.Create().
		SetName("reset-key").
		SetKeyHash("reset-hash").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetQuotaUsd(10).
		SetUsedQuota(1).
		SetBalanceAlertEnabled(true).
		SetBalanceAlertEmail("reset@example.com").
		SetBalanceAlertThreshold(1).
		SetBalanceAlertNotified(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create reset key: %v", err)
	}
	skipKey, err := db.APIKey.Create().
		SetName("skip-key").
		SetKeyHash("skip-hash").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetQuotaUsd(10).
		SetUsedQuota(1).
		SetBalanceAlertEnabled(true).
		SetBalanceAlertEmail("skip@example.com").
		SetBalanceAlertThreshold(1).
		Save(ctx)
	if err != nil {
		t.Fatalf("create skip key: %v", err)
	}

	recorder := NewRecorder(db, 1)
	oldGo := recorderGo
	recorderGo = func(name string, fn func()) {
		if name != "api_key_balance_alert_check" {
			t.Fatalf("unexpected goroutine name %q", name)
		}
		fn()
	}
	defer func() { recorderGo = oldGo }()

	var inputs []APIKeyBalanceAlertInput
	recorder.SetAPIKeyBalanceAlertCallback(func(input APIKeyBalanceAlertInput) {
		inputs = append(inputs, input)
	})
	recorder.scheduleAPIKeyBalanceAlertCheck([]UsageRecord{
		{APIKeyID: alertKey.ID, BilledCost: 0.25},
		{APIKeyID: resetKey.ID, BilledCost: 0.25},
		{APIKeyID: skipKey.ID, BilledCost: 0.25},
	})
	if len(inputs) != 1 {
		t.Fatalf("alert callbacks = %#v, want one", inputs)
	}
	input := inputs[0]
	if input.KeyID != alertKey.ID || input.UserID != user.ID || input.UserEmail != user.Email {
		t.Fatalf("alert input = %#v", input)
	}
	if input.AlertEmail != "alert@example.com" || input.Remaining != 0.5 || input.Threshold != 1 {
		t.Fatalf("alert threshold fields = %#v", input)
	}
	alertAfter, err := db.APIKey.Get(ctx, alertKey.ID)
	if err != nil {
		t.Fatalf("get alert key: %v", err)
	}
	if !alertAfter.BalanceAlertNotified {
		t.Fatalf("alert key should be marked notified")
	}
	resetAfter, err := db.APIKey.Get(ctx, resetKey.ID)
	if err != nil {
		t.Fatalf("get reset key: %v", err)
	}
	if resetAfter.BalanceAlertNotified {
		t.Fatalf("reset key should be marked unnotified")
	}

	recorder.SetAPIKeyBalanceAlertCallback(nil)
	recorder.scheduleAPIKeyBalanceAlertCheck([]UsageRecord{{APIKeyID: alertKey.ID, BilledCost: 1}})
	recorder.SetAPIKeyBalanceAlertCallback(func(APIKeyBalanceAlertInput) {})
	recorder.scheduleAPIKeyBalanceAlertCheck([]UsageRecord{{APIKeyID: alertKey.ID, BilledCost: 0}})
	recorder.checkAPIKeyBalanceAlerts(ctx, nil, func(APIKeyBalanceAlertInput) {
		t.Fatalf("empty key list should not invoke callback")
	})
}

func TestAPIKeyBalanceAlertErrorBranches(t *testing.T) {
	ctx := context.Background()

	loadDB := openBillingRecorderDB(t, "billing_alert_load_error")
	user, group, _, _ := createBillingFixture(t, ctx, loadDB, "alert-load-error")
	key, err := loadDB.APIKey.Create().
		SetName("load-error-key").
		SetKeyHash("load-error-hash").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create load error key: %v", err)
	}
	if err := loadDB.Close(); err != nil {
		t.Fatalf("close load db: %v", err)
	}
	NewRecorder(loadDB, 1).checkAPIKeyBalanceAlerts(ctx, []int{key.ID}, func(APIKeyBalanceAlertInput) {
		t.Fatalf("load error should not invoke callback")
	})

	t.Run("reset update error", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_alert_reset_error")
		defer closeBillingDB(t, db)
		user, group, _, _ := createBillingFixture(t, ctx, db, "alert-reset-error")
		resetKey, err := db.APIKey.Create().
			SetName("reset-error-key").
			SetKeyHash("reset-error-hash").
			SetUserID(user.ID).
			SetGroupID(group.ID).
			SetQuotaUsd(10).
			SetUsedQuota(1).
			SetBalanceAlertEnabled(true).
			SetBalanceAlertEmail("reset-error@example.com").
			SetBalanceAlertThreshold(1).
			SetBalanceAlertNotified(true).
			Save(ctx)
		if err != nil {
			t.Fatalf("create reset error key: %v", err)
		}
		db.APIKey.Use(errorAPIKeyMutationHook(ent.OpUpdateOne, errors.New("reset failed")))
		NewRecorder(db, 1).checkAPIKeyBalanceAlerts(ctx, []int{resetKey.ID}, func(APIKeyBalanceAlertInput) {
			t.Fatalf("reset error should not invoke callback")
		})
	})

	t.Run("mark update error", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_alert_mark_error")
		defer closeBillingDB(t, db)
		user, group, _, _ := createBillingFixture(t, ctx, db, "alert-mark-error")
		alertKey, err := db.APIKey.Create().
			SetName("mark-error-key").
			SetKeyHash("mark-error-hash").
			SetUserID(user.ID).
			SetGroupID(group.ID).
			SetQuotaUsd(5).
			SetUsedQuota(4.5).
			SetBalanceAlertEnabled(true).
			SetBalanceAlertEmail("mark-error@example.com").
			SetBalanceAlertThreshold(1).
			Save(ctx)
		if err != nil {
			t.Fatalf("create mark error key: %v", err)
		}
		db.APIKey.Use(errorAPIKeyMutationHook(ent.OpUpdate, errors.New("mark failed")))
		NewRecorder(db, 1).checkAPIKeyBalanceAlerts(ctx, []int{alertKey.ID}, func(APIKeyBalanceAlertInput) {
			t.Fatalf("mark error should not invoke callback")
		})
	})

	t.Run("mark affected zero", func(t *testing.T) {
		db := openBillingRecorderDB(t, "billing_alert_mark_zero")
		defer closeBillingDB(t, db)
		user, group, _, _ := createBillingFixture(t, ctx, db, "alert-mark-zero")
		alertKey, err := db.APIKey.Create().
			SetName("mark-zero-key").
			SetKeyHash("mark-zero-hash").
			SetUserID(user.ID).
			SetGroupID(group.ID).
			SetQuotaUsd(5).
			SetUsedQuota(4.5).
			SetBalanceAlertEnabled(true).
			SetBalanceAlertEmail("mark-zero@example.com").
			SetBalanceAlertThreshold(1).
			Save(ctx)
		if err != nil {
			t.Fatalf("create mark zero key: %v", err)
		}
		db.APIKey.Use(func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
				if mutation.Op().Is(ent.OpUpdate) {
					return 0, nil
				}
				return next.Mutate(ctx, mutation)
			})
		})
		NewRecorder(db, 1).checkAPIKeyBalanceAlerts(ctx, []int{alertKey.ID}, func(APIKeyBalanceAlertInput) {
			t.Fatalf("affected zero should not invoke callback")
		})
	})
}

func TestApplyUsageChargesErrorBranches(t *testing.T) {
	db := openBillingRecorderDB(t, "billing_apply_errors")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, _, _, _ := createBillingFixture(t, ctx, db, "apply-errors")

	tx, err := db.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx for user error: %v", err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := applyUsageCharges(cancelCtx, tx, []UsageRecord{{UserID: user.ID, ActualCost: 1}}); err == nil {
		t.Fatalf("applyUsageCharges should return user update error")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback user error tx: %v", err)
	}

	tx, err = db.Tx(ctx)
	if err != nil {
		t.Fatalf("start tx for key error: %v", err)
	}
	if err := applyUsageCharges(ctx, tx, []UsageRecord{{APIKeyID: 9999, BilledCost: 1}}); err == nil {
		t.Fatalf("applyUsageCharges should return api key update error")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback key error tx: %v", err)
	}
}

func createBillingTestUser(t *testing.T, ctx context.Context, db *ent.Client, email string) *ent.User {
	t.Helper()
	user, err := db.User.Create().
		SetEmail(email).
		SetPasswordHash("secret").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func openBillingRecorderDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	safeName := strings.NewReplacer("/", "_", " ", "_").Replace(name)
	return testdb.OpenMemoryEnt(t, safeName, schema.WithGlobalUniqueID(false))
}

func closeBillingDB(t *testing.T, db *ent.Client) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}

func createBillingFixture(t *testing.T, ctx context.Context, db *ent.Client, suffix string) (*ent.User, *ent.Group, *ent.Account, *ent.APIKey) {
	t.Helper()
	user := createBillingTestUser(t, ctx, db, "billing-"+suffix+"@example.com")
	group, err := db.Group.Create().
		SetName("group-" + suffix).
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("account-" + suffix).
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("key-" + suffix).
		SetKeyHash("hash-" + suffix).
		SetUserID(user.ID).
		SetGroupID(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return user, group, account, key
}

func billingRecordForFixture(eventID string, user *ent.User, group *ent.Group, account *ent.Account, key *ent.APIKey) UsageRecord {
	keyID := 0
	if key != nil {
		keyID = key.ID
	}
	return UsageRecord{
		BillingEventID: eventID,
		UserID:         user.ID,
		UserEmail:      user.Email,
		APIKeyID:       keyID,
		AccountID:      account.ID,
		GroupID:        group.ID,
		Platform:       "openai",
		Model:          "gpt-5",
		UsageMetadata:  map[string]string{"event": eventID},
	}
}

func waitForUsageLog(t *testing.T, ctx context.Context, db *ent.Client, eventID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		exists, err := db.UsageLog.Query().
			Where(usagelog.BillingEventIDEQ(eventID)).
			Exist(ctx)
		if err != nil {
			t.Fatalf("query usage log %q: %v", eventID, err)
		}
		if exists {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("usage log %q was not persisted", eventID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("channel was not closed")
	}
}

func errorAPIKeyMutationHook(op ent.Op, err error) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op().Is(op) {
				return nil, err
			}
			return next.Mutate(ctx, mutation)
		})
	}
}

func errorUserMutationHook(op ent.Op, err error) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op().Is(op) {
				return nil, err
			}
			return next.Mutate(ctx, mutation)
		})
	}
}

type fakeUsageLogRows struct {
	nexts []bool
	idx   int
	scan  func(dest ...any) error
	err   error
}

func (r *fakeUsageLogRows) Close() error {
	return nil
}

func (r *fakeUsageLogRows) Next() bool {
	if r.idx >= len(r.nexts) {
		return false
	}
	next := r.nexts[r.idx]
	r.idx++
	return next
}

func (r *fakeUsageLogRows) Scan(dest ...any) error {
	if r.scan != nil {
		return r.scan(dest...)
	}
	return nil
}

func (r *fakeUsageLogRows) Err() error {
	return r.err
}
