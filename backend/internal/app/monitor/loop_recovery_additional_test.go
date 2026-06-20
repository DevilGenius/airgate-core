package monitor

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

func TestAdditionalAggregatorLoopProcessesOperations(t *testing.T) {
	repo := &monitorRepoStub{
		insertCh:        make(chan struct{}, 2),
		insertRequestCh: make(chan struct{}, 1),
		resolveCh:       make(chan struct{}, 1),
	}
	publisher := &monitorPublisherStub{}
	service := NewService(repo,
		WithQueueSize(8),
		WithFlushBatchSize(1),
		WithFlushInterval(time.Hour),
		WithEventPublisher(publisher),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.runAggregatorLoop(ctx)
		close(done)
	}()

	service.queue <- queuedOperation{Kind: queuedOperationRecord, Event: QueuedEvent{}}
	service.queue <- queuedOperation{Kind: queuedOperationRecord, Event: QueuedEvent{Event: Event{Hash: "event-hash"}}}
	waitForMonitorTestSignal(t, repo.insertCh, "event insert")

	service.queue <- queuedOperation{Kind: queuedOperationRecordRequest, RequestEvent: QueuedRequestEvent{}}
	service.queue <- queuedOperation{Kind: queuedOperationRecordRequest, RequestEvent: QueuedRequestEvent{RequestEvent: RequestEvent{Hash: "request-hash"}}}
	waitForMonitorTestSignal(t, repo.insertRequestCh, "request insert")

	service.queue <- queuedOperation{Kind: queuedOperationResolve, Resolve: monitoring.ResolveQuery{Hash: "event-hash"}}
	waitForMonitorTestSignal(t, repo.resolveCh, "resolve")
	service.queue <- queuedOperation{Kind: "unknown"}

	cancel()
	waitForMonitorTestSignal(t, done, "aggregator exit")
	if service.flushedEvents.Load() != 2 || len(repo.inserted) != 1 || len(repo.insertedRequests) != 1 || len(repo.resolvedQueries) != 1 {
		t.Fatalf("aggregator state flushed=%d inserted=%d requests=%d resolved=%d",
			service.flushedEvents.Load(), len(repo.inserted), len(repo.insertedRequests), len(repo.resolvedQueries))
	}
	if !containsReason(publisher.reasons, "recorded") || !containsReason(publisher.reasons, "request_recorded") || !containsReason(publisher.reasons, "resolved") {
		t.Fatalf("publisher reasons = %v", publisher.reasons)
	}
}

func TestAdditionalAggregatorTailFlushAndErrorBranches(t *testing.T) {
	repo := &monitorRepoStub{insertCh: make(chan struct{}, 1)}
	service := NewService(repo, WithQueueSize(4), WithFlushBatchSize(10), WithFlushInterval(time.Hour))
	service.queue <- queuedOperation{Kind: queuedOperationRecord, Event: QueuedEvent{Event: Event{Hash: "tail"}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.runAggregatorLoop(ctx)
		close(done)
	}()
	waitForQueueDrain(t, service)
	cancel()
	waitForMonitorTestSignal(t, done, "tail aggregator exit")
	if len(repo.inserted) != 1 {
		t.Fatalf("tail flush inserted batches = %d", len(repo.inserted))
	}

	errRepo := &monitorRepoStub{
		insertErr:        errors.New("insert failed"),
		insertRequestErr: errors.New("insert request failed"),
		resolveQueryErr:  errors.New("resolve failed"),
	}
	errService := NewService(errRepo)
	if err := errService.flushBatch(t.Context(), []QueuedEvent{{Event: Event{Hash: "h"}}}); err == nil {
		t.Fatal("flushBatch insert error returned nil")
	}
	if err := errService.flushRequestBatch(t.Context(), []QueuedRequestEvent{{RequestEvent: RequestEvent{Hash: "rh"}}}); err == nil {
		t.Fatal("flushRequestBatch insert error returned nil")
	}
	errService.resolveBySubject(t.Context(), monitoring.ResolveQuery{Hash: "h"})
	if len(errRepo.resolvedQueries) != 1 {
		t.Fatalf("resolveBySubject did not call repo on error: %d", len(errRepo.resolvedQueries))
	}
	if err := (*Service)(nil).flushBatch(t.Context(), nil); err != nil {
		t.Fatalf("nil empty flushBatch = %v", err)
	}
	if err := NewService(nil).flushRequestBatch(t.Context(), []QueuedRequestEvent{{RequestEvent: RequestEvent{Hash: "rh"}}}); err != nil {
		t.Fatalf("nil repo flushRequestBatch = %v", err)
	}
}

func TestAdditionalServiceOptionsAndPassthroughErrors(t *testing.T) {
	publisher := &monitorPublisherStub{}
	service := NewService(nil,
		nil,
		WithFlushBatchSize(3),
		WithFlushInterval(2*time.Second),
		WithRetention(3*time.Hour),
		WithEventPublisher(publisher),
		func(s *Service) { s.queue = nil },
	)
	if service.flushBatchSize != 3 || service.flushInterval != 2*time.Second || service.retention != 3*time.Hour || service.queue == nil {
		t.Fatalf("NewService options = batch=%d interval=%s retention=%s queue=%v", service.flushBatchSize, service.flushInterval, service.retention, service.queue)
	}
	service.publishMonitorChanged("manual")
	if !containsReason(publisher.reasons, "manual") {
		t.Fatalf("publish reasons = %v", publisher.reasons)
	}

	var nilService *Service
	if list, err := nilService.List(t.Context(), ListFilter{}); err != nil || len(list.List) != 0 {
		t.Fatalf("nil List = %+v %v", list, err)
	}
	if list, err := nilService.ListRequests(t.Context(), RequestListFilter{}); err != nil || len(list.List) != 0 {
		t.Fatalf("nil ListRequests = %+v %v", list, err)
	}
	if deleted, err := nilService.ClearRequestEvents(t.Context(), nil); err != nil || deleted != 0 {
		t.Fatalf("nil ClearRequestEvents = %d %v", deleted, err)
	}
	if summary, err := nilService.Summary(t.Context()); err != nil || summary.ActiveTotal != 0 {
		t.Fatalf("nil Summary = %+v %v", summary, err)
	}
	if summary, err := nilService.RequestSummary(t.Context()); err != nil || summary.ActiveTotal != 0 {
		t.Fatalf("nil RequestSummary = %+v %v", summary, err)
	}
	if err := nilService.Resolve(t.Context(), 1); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("nil Resolve = %v", err)
	}

	clearErr := errors.New("clear failed")
	if _, err := NewService(&monitorRepoStub{
		clearRequestEvents: func(context.Context, *time.Time) (int, error) { return 0, clearErr },
	}).ClearRequestEvents(t.Context(), nil); !errors.Is(err, clearErr) {
		t.Fatalf("ClearRequestEvents error = %v", err)
	}
	getErr := errors.New("get failed")
	if err := NewService(&monitorRepoStub{get: func(context.Context, int) (Event, error) { return Event{}, getErr }}).Resolve(t.Context(), 1); !errors.Is(err, getErr) {
		t.Fatalf("Resolve get error = %v", err)
	}
	resolveErr := errors.New("resolve failed")
	if err := NewService(&monitorRepoStub{
		get: func(context.Context, int) (Event, error) {
			return Event{Type: monitoring.TypePluginError, Severity: monitoring.SeverityError, RecoveryMode: monitoring.RecoveryModeManual}, nil
		},
		resolveErr: resolveErr,
	}).Resolve(t.Context(), 1); !errors.Is(err, resolveErr) {
		t.Fatalf("Resolve repo error = %v", err)
	}
	if normalizeListLimit(7) != 7 {
		t.Fatal("normalizeListLimit should preserve in-range limit")
	}
}

func TestAdditionalRecoveryRedisBranches(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	service := NewService(nil, WithRedis(rdb))
	nowUnix := strconv.FormatInt(time.Now().Unix(), 10)
	future := time.Now().Add(time.Hour).Unix()
	mock.ExpectZRemRangeByScore(monitorRecoveryZSetKey, "-inf", nowUnix).SetVal(1)
	mock.ExpectZRangeByScoreWithScores(monitorRecoveryZSetKey, &redis.ZRangeBy{Min: nowUnix, Max: "+inf"}).SetVal([]redis.Z{
		{Member: "loaded-hash", Score: float64(future)},
		{Member: 123, Score: float64(future)},
		{Member: "", Score: float64(future)},
	})
	service.loadRecoverySnapshot(t.Context())
	if !service.recovery.has("loaded-hash", time.Now()) {
		t.Fatal("loaded recovery hash missing from snapshot")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("load recovery expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	service = NewService(nil, WithRedis(rdb))
	nowUnix = strconv.FormatInt(time.Now().Unix(), 10)
	mock.ExpectZRemRangeByScore(monitorRecoveryZSetKey, "-inf", nowUnix).SetVal(0)
	mock.ExpectZRangeByScoreWithScores(monitorRecoveryZSetKey, &redis.ZRangeBy{Min: nowUnix, Max: "+inf"}).SetErr(errors.New("redis down"))
	service.loadRecoverySnapshot(t.Context())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("load recovery error expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	service = NewService(nil, WithRedis(rdb))
	service.recovery.remember("expired", time.Now().Add(-time.Hour))
	nowUnix = strconv.FormatInt(time.Now().Unix(), 10)
	mock.ExpectZRemRangeByScore(monitorRecoveryZSetKey, "-inf", nowUnix).SetVal(1)
	service.pruneRecoverySnapshot(t.Context())
	if service.recovery.has("expired", time.Now()) {
		t.Fatal("expired recovery hash still active")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("prune recovery expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	service = NewService(nil, WithRedis(rdb))
	service.recovery.remember("forget-me", time.Now().Add(time.Hour))
	mock.ExpectZRem(monitorRecoveryZSetKey, "forget-me").SetVal(1)
	service.forgetRecoveryQuery(t.Context(), monitoring.ResolveQuery{Hash: "forget-me"})
	if service.recovery.has("forget-me", time.Now()) {
		t.Fatal("forgotten recovery hash still active")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("forget recovery expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	service = NewService(nil, WithRedis(rdb))
	mock.ExpectEvalSha(monitorNotifyUnlockScript.Hash(), []string{monitorNotifyLockKey(42)}, "token").SetVal(int64(1))
	service.releaseNotify(t.Context(), 42, "token")
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("release notify expectations: %v", err)
	}
}

func TestAdditionalWorkerLoopAndNotifyBranches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewService(nil).runWorkerLoop(ctx)
	NewService(&monitorRepoStub{}).runWorkerLoop(ctx)

	service := NewService(&monitorRepoStub{notifyErr: errors.New("scan failed")}, WithNotifier(&monitorNotifierStub{configured: true}))
	service.runNotifyOnce(t.Context())

	service = NewService(&monitorRepoStub{}, WithNotifier(&monitorNotifierStub{configErr: errors.New("config failed")}))
	service.runNotifyOnce(t.Context())

	rdb, mock := redismock.NewClientMock()
	repo := &monitorRepoStub{notifyDue: []Event{{ID: 31, Severity: monitoring.SeverityError, Title: "skip"}}}
	service = NewService(repo, WithNotifier(&monitorNotifierStub{configured: true}), WithRedis(rdb))
	mock.Regexp().ExpectSetNX(monitorNotifyLockKey(31), ".+", notifyLockTTL).SetVal(false)
	service.runNotifyOnce(t.Context())
	if repo.markedNotifiedID != 0 {
		t.Fatalf("unclaimed notification marked notified: %d", repo.markedNotifiedID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("claim false expectations: %v", err)
	}

	repo = &monitorRepoStub{notifyDue: []Event{{ID: 32, Severity: monitoring.SeverityError, Title: "fail"}}, markFailedErr: errors.New("mark failed")}
	service = NewService(repo, WithNotifier(&monitorNotifierStub{configured: true, err: errors.New("send failed")}))
	service.runNotifyOnce(t.Context())

	repo = &monitorRepoStub{notifyDue: []Event{{ID: 33, Severity: monitoring.SeverityError, Title: "success"}}, markNotifiedErr: errors.New("mark notified failed")}
	service = NewService(repo, WithNotifier(&monitorNotifierStub{configured: true}))
	service.runNotifyOnce(t.Context())
	if repo.markedNotifiedID != 33 {
		t.Fatalf("mark notified error branch id = %d", repo.markedNotifiedID)
	}
}

func TestAdditionalSuperviseLoopAndSanitizerBranches(t *testing.T) {
	service := NewService(nil, WithQueueSize(4))
	var panics atomic.Int64
	runs := 0
	service.superviseLoop(t.Context(), "test_loop", &panics, func() {
		runs++
		if runs == 1 {
			panic("boom")
		}
	})
	if runs != 2 || panics.Load() != 1 || service.queuedEvents.Load() != 1 {
		t.Fatalf("supervise runs=%d panics=%d queued=%d", runs, panics.Load(), service.queuedEvents.Load())
	}

	if got := sanitizeMap(map[string]interface{}{"secret": "value"}, 4); got["_truncated"] != true {
		t.Fatalf("deep sanitize map = %+v", got)
	}
	for _, value := range []interface{}{nil, true, float32(1.5), int8(1), int16(2), int32(3), int64(4), uint(5), uint8(6), uint16(7), uint32(8), uint64(9)} {
		if sanitizeValue(value, 0) == nil && value != nil {
			t.Fatalf("sanitizeValue(%T) returned nil", value)
		}
	}
	if got := sanitizeValue(struct{ Token string }{Token: "secret"}, 0); !strings.Contains(got.(string), "secret") {
		t.Fatalf("sanitize default value = %#v", got)
	}

	service = NewService(nil)
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	success := service.normalizeInput(monitoring.EventInput{
		Type:        monitoring.TypeSchedulerError,
		Severity:    monitoring.SeverityError,
		SubjectType: monitoring.SubjectScheduler,
		SubjectID:   "openai",
		ErrorCode:   "no_available_account",
		ObservedAt:  now,
		Detail:      map[string]interface{}{"model": "gpt-5"},
	}).Event
	if success.RecoveryMode != monitoring.RecoveryModeSuccess || success.AutoResolveAt == nil {
		t.Fatalf("success recovery event = %+v", success)
	}
	external := service.normalizeInput(monitoring.EventInput{
		Type:       monitoring.TypeUpstreamAccountError,
		Severity:   monitoring.SeverityError,
		ErrorCode:  "account_disabled",
		ObservedAt: now,
	}).Event
	if external.RecoveryMode != monitoring.RecoveryModeExternal || external.AutoResolveAt != nil {
		t.Fatalf("external recovery event = %+v", external)
	}
	if detailWithRequestPath(map[string]interface{}{"a": 1}, "")["a"] != 1 {
		t.Fatal("detailWithRequestPath blank path should return original detail")
	}
	if requestHashFor("custom", RequestEvent{}) == requestHashFor("", RequestEvent{}) {
		t.Fatal("custom request hash should differ from default hash")
	}
	if hashFor("custom", Event{}) == hashFor("", Event{}) {
		t.Fatal("custom event hash should differ from default hash")
	}
	if recoveryModeForEvent(Event{Severity: monitoring.SeverityError, RecoveryMode: monitoring.RecoveryModeManual}) != monitoring.RecoveryModeManual {
		t.Fatal("explicit recovery mode should be preserved")
	}
	if monitorDetailString(map[string]interface{}{"s": " value "}, "s") != "value" {
		t.Fatal("monitorDetailString should trim strings")
	}
}

func TestAdditionalRecordRecoverySuccessBranches(t *testing.T) {
	service := NewService(nil, WithQueueSize(1))
	queries := recoveryResolveQueries(monitoring.RecoverySuccess{Platform: "openai", Model: "gpt-5"})
	service.recovery.remember(queries[0].Hash, time.Now().Add(time.Hour))
	service.recovery.remember(queries[1].Hash, time.Now().Add(time.Hour))
	service.RecordRecoverySuccess(t.Context(), monitoring.RecoverySuccess{Platform: "openai", Model: "gpt-5"})
	service.RecordRecoverySuccess(t.Context(), monitoring.RecoverySuccess{Platform: "openai", Model: "gpt-5"})
	if service.queuedEvents.Load() != 1 || service.droppedEvents.Load() == 0 {
		t.Fatalf("recovery queue/drops = %d/%d", service.queuedEvents.Load(), service.droppedEvents.Load())
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	service.RecordRecoverySuccess(canceled, monitoring.RecoverySuccess{Platform: "openai", Model: "gpt-5"})

	noActive := NewService(nil)
	noActive.RecordRecoverySuccess(t.Context(), monitoring.RecoverySuccess{Platform: "openai", Model: "gpt-5"})
	if noActive.queuedEvents.Load() != 0 {
		t.Fatalf("inactive recovery queued = %d", noActive.queuedEvents.Load())
	}
	var nilService *Service
	nilService.Record(t.Context(), monitoring.EventInput{})
	nilService.Record(t.Context(), monitoring.EventInput{Type: monitoring.TypeSystemError})
	nilService.RecordRequest(t.Context(), requestmonitoring.EventInput{})
}

func waitForMonitorTestSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitForQueueDrain(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for monitor queue drain")
		case <-tick.C:
			if len(service.queue) == 0 {
				return
			}
		}
	}
}
