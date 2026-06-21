package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

func TestNormalizeInputDowngradesSingleAccountSeverity(t *testing.T) {
	accountID := 4114
	service := NewService(nil)

	event := service.normalizeInput(monitoring.EventInput{
		Type:        monitoring.TypeUpstreamAccountError,
		Severity:    monitoring.SeverityCritical,
		SubjectType: monitoring.SubjectAccount,
		AccountID:   &accountID,
		SubjectID:   "4114",
	}).Event

	if event.Severity != monitoring.SeverityWarning {
		t.Fatalf("severity = %q, want warning", event.Severity)
	}
}

func TestNormalizeInputKeepsNonAccountSeverity(t *testing.T) {
	service := NewService(nil)

	event := service.normalizeInput(monitoring.EventInput{
		Type:        monitoring.TypeSchedulerError,
		Severity:    monitoring.SeverityError,
		SubjectType: monitoring.SubjectScheduler,
		SubjectID:   "openai",
	}).Event

	if event.Severity != monitoring.SeverityError {
		t.Fatalf("severity = %q, want error", event.Severity)
	}
}

func TestServiceOptionsRecordAndNormalize(t *testing.T) {
	service := NewService(nil,
		WithQueueSize(1),
		WithFlushBatchSize(0),
		WithFlushInterval(0),
		WithRetention(0),
	)
	if cap(service.queue) != 1 || service.flushBatchSize != defaultFlushBatchSize || service.flushInterval != defaultFlushInterval || service.retention != defaultRetention {
		t.Fatalf("service options not normalized: queue=%d batch=%d interval=%s retention=%s", cap(service.queue), service.flushBatchSize, service.flushInterval, service.retention)
	}

	accountID := 42
	observed := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	event := service.normalizeInput(monitoring.EventInput{
		Type:                "unknown",
		Severity:            monitoring.SeverityCritical,
		Source:              "",
		SubjectType:         monitoring.SubjectAccount,
		AccountID:           &accountID,
		Title:               "Authorization: Bearer abc.def sk-12345678 user@example.com",
		Message:             "api_key=secret refresh_token=rt",
		AccountNameSnapshot: stringsOfLength("account", maxSnapshotLength+10),
		Platform:            stringsOfLength("platform", maxPlatformLength+10),
		ErrorCode:           stringsOfLength("code", maxCodeLength+10),
		ObservedAt:          observed,
		Detail: map[string]interface{}{
			"authorization": "Bearer token",
			"list":          []interface{}{"same", "same", "other"},
			"nested":        map[string]interface{}{"token": "secret"},
		},
	}).Event
	if event.Type != monitoring.TypeSystemError || event.Source != monitoring.SourceMonitorWorker ||
		event.SubjectID != "42" || event.Status != monitoring.StatusActive || event.CreatedAt != observed {
		t.Fatalf("normalized event = %+v", event)
	}
	if event.Title == "" || event.Title == "Authorization: Bearer abc.def sk-12345678 user@example.com" || event.Message == "api_key=secret refresh_token=rt" {
		t.Fatalf("scrubbing failed title=%q message=%q", event.Title, event.Message)
	}
	if event.Detail["authorization"] != "[REDACTED]" {
		t.Fatalf("sensitive detail = %+v", event.Detail)
	}

	request := service.normalizeRequestInput(requestmonitoring.EventInput{
		Type:           requestmonitoring.TypeClientClosed,
		Severity:       "bad",
		Source:         "",
		Method:         " post ",
		Endpoint:       stringsOfLength("/v1/", maxEndpointLength+10),
		RequestPath:    "/actual/request/path",
		DurationMS:     -5,
		ObservedAt:     observed,
		HTTPStatus:     &accountID,
		UpstreamStatus: &accountID,
		Detail:         map[string]interface{}{"cookie": "session=secret"},
	}).RequestEvent
	if request.Type != requestmonitoring.TypeClientClosed || request.Severity != requestmonitoring.SeverityInfo ||
		request.Source != monitoring.SourceForwarder || request.Method != "POST" || request.DurationMS != 0 ||
		request.Detail["cookie"] != "[REDACTED]" || request.Detail["request_path"] != "/actual/request/path" {
		t.Fatalf("normalized request = %+v detail=%+v", request, request.Detail)
	}

	service.Record(t.Context(), monitoring.EventInput{Type: monitoring.TypeSystemError})
	if got := service.queuedEvents.Load(); got != 1 {
		t.Fatalf("queued events = %d, want 1", got)
	}
	service.Record(t.Context(), monitoring.EventInput{Type: monitoring.TypeSystemError})
	if got := service.droppedEvents.Load(); got != 1 {
		t.Fatalf("dropped events = %d, want 1", got)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	service.Record(canceled, monitoring.EventInput{Type: monitoring.TypeSystemError})
	service.RecordRequest(canceled, requestmonitoring.EventInput{Type: requestmonitoring.TypeAPIRequestError})
	service.ResolveBySubject(canceled, monitoring.ResolveQuery{Hash: "hash"})
	if got := service.queuedEvents.Load(); got != 1 {
		t.Fatalf("queued events after canceled calls = %d, want 1", got)
	}
}

func TestRepositoryPassthroughResolveAndFlush(t *testing.T) {
	repo := &monitorRepoStub{
		get: func(_ context.Context, id int) (Event, error) {
			return Event{ID: id, Type: monitoring.TypeSchedulerError, Severity: monitoring.SeverityError, SubjectType: monitoring.SubjectScheduler, ErrorCode: "manual", RecoveryMode: monitoring.RecoveryModeManual}, nil
		},
		list: func(_ context.Context, filter ListFilter) (ListResult, error) {
			if filter.Limit != maxListLimit {
				t.Fatalf("List limit = %d, want maxListLimit", filter.Limit)
			}
			return ListResult{List: []Event{{ID: 1}}}, nil
		},
		listRequests: func(_ context.Context, filter RequestListFilter) (RequestListResult, error) {
			if filter.Limit != defaultListLimit {
				t.Fatalf("ListRequests limit = %d, want defaultListLimit", filter.Limit)
			}
			return RequestListResult{List: []RequestEvent{{ID: 2}}}, nil
		},
		clearRequestEvents: func(context.Context, *time.Time) (int, error) { return 3, nil },
		summary:            func(context.Context) (Summary, error) { return Summary{ActiveTotal: 4}, nil },
		requestSummary:     func(context.Context) (Summary, error) { return Summary{ActiveTotal: 5}, nil },
	}
	publisher := &monitorPublisherStub{}
	service := NewService(repo, WithEventPublisher(publisher))

	if _, err := service.Get(t.Context(), 7); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if list, err := service.List(t.Context(), ListFilter{Limit: 999}); err != nil || len(list.List) != 1 {
		t.Fatalf("List = %+v err=%v", list, err)
	}
	if list, err := service.ListRequests(t.Context(), RequestListFilter{}); err != nil || len(list.List) != 1 {
		t.Fatalf("ListRequests = %+v err=%v", list, err)
	}
	if deleted, err := service.ClearRequestEvents(t.Context(), nil); err != nil || deleted != 3 || publisher.reasons[len(publisher.reasons)-1] != "request_cleared" {
		t.Fatalf("ClearRequestEvents deleted=%d err=%v reasons=%v", deleted, err, publisher.reasons)
	}
	if summary, err := service.Summary(t.Context()); err != nil || summary.ActiveTotal != 4 {
		t.Fatalf("Summary = %+v err=%v", summary, err)
	}
	if summary, err := service.RequestSummary(t.Context()); err != nil || summary.ActiveTotal != 5 {
		t.Fatalf("RequestSummary = %+v err=%v", summary, err)
	}
	if err := service.Resolve(t.Context(), 7); err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if repo.resolvedID != 7 || publisher.reasons[len(publisher.reasons)-1] != "resolved" {
		t.Fatalf("resolvedID=%d reasons=%v", repo.resolvedID, publisher.reasons)
	}

	if err := service.flushBatch(t.Context(), []QueuedEvent{{Event: Event{Hash: "h"}}}); err != nil {
		t.Fatalf("flushBatch returned error: %v", err)
	}
	if err := service.flushRequestBatch(t.Context(), []QueuedRequestEvent{{RequestEvent: RequestEvent{Hash: "rh"}}}); err != nil {
		t.Fatalf("flushRequestBatch returned error: %v", err)
	}
	service.resolveBySubject(t.Context(), monitoring.ResolveQuery{Hash: "h"})
	if len(repo.inserted) != 1 || len(repo.insertedRequests) != 1 || len(repo.resolvedQueries) != 1 {
		t.Fatalf("repo calls inserted=%d request=%d resolved=%d", len(repo.inserted), len(repo.insertedRequests), len(repo.resolvedQueries))
	}

	var nilService *Service
	if _, err := nilService.Get(t.Context(), 1); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("nil Get error = %v", err)
	}
	if err := (&Service{repo: &monitorRepoStub{get: func(context.Context, int) (Event, error) {
		return Event{Type: monitoring.TypeUpstreamAccountError, Severity: monitoring.SeverityWarning}, nil
	}}}).Resolve(t.Context(), 1); !errors.Is(err, ErrEventNotRecoverable) {
		t.Fatalf("non recoverable Resolve error = %v", err)
	}
}

func TestRecoverySnapshotAndQueries(t *testing.T) {
	now := time.Now().UTC()
	recovery := newRecoverySnapshot()
	recovery.remember(" keep ", now.Add(time.Hour))
	recovery.remember("expired", now.Add(-time.Hour))
	if !recovery.has("keep", now) || recovery.has("expired", now) || recovery.has("", now) {
		t.Fatalf("recovery has keep=%v expired=%v", recovery.has("keep", now), recovery.has("expired", now))
	}
	recovery.pruneExpired(now)
	if recovery.has("expired", now) {
		t.Fatal("expired recovery entry should be pruned")
	}
	recovery.merge(map[string]time.Time{"merged": now.Add(time.Hour)}, now)
	if !recovery.has("merged", now) {
		t.Fatal("merged recovery entry missing")
	}
	recovery.forget("keep")
	if recovery.has("keep", now) {
		t.Fatal("forgotten recovery entry still present")
	}

	event := Event{
		Type:        monitoring.TypeSchedulerError,
		Severity:    monitoring.SeverityError,
		SubjectType: monitoring.SubjectScheduler,
		SubjectID:   "openai",
		ErrorCode:   "no_available_account",
		Detail:      map[string]interface{}{"model": "gpt-5"},
	}
	hash, ok := recoveryHashForEvent(event)
	if !ok || hash == "" {
		t.Fatalf("recoveryHashForEvent hash=%q ok=%v", hash, ok)
	}
	if recoveryModeForEvent(event) != monitoring.RecoveryModeSuccess {
		t.Fatalf("recoveryModeForEvent = %q", recoveryModeForEvent(event))
	}
	if !nonManualRecoverableEvent(Event{RecoveryMode: monitoring.RecoveryModeExternal}) {
		t.Fatal("external recovery should be non-manual")
	}
	if recoveryFallbackAt(nil, now).Sub(now) != successRecoveryFallbackWindow {
		t.Fatal("fallback recovery window mismatch")
	}
	if !recoveryKeyExpiresAt(Event{ExpiresAt: now.Add(time.Hour)}).Equal(now.Add(time.Hour)) {
		t.Fatal("recoveryKeyExpiresAt should prefer ExpiresAt")
	}

	queries := recoveryResolveQueries(monitoring.RecoverySuccess{Platform: "openai", Model: "gpt-5"})
	if len(queries) != len(recoverySchedulerErrorCodes) {
		t.Fatalf("recovery queries = %+v", queries)
	}
	if monitorDetailString(map[string]interface{}{"a": float64(3)}, "a") != "3" ||
		monitorDetailString(map[string]interface{}{"a": int64(4)}, "a") != "4" {
		t.Fatal("monitorDetailString numeric conversion failed")
	}

	service := NewService(nil, WithQueueSize(2))
	service.recovery.remember(queries[0].Hash, now.Add(time.Hour))
	service.RecordRecoverySuccess(t.Context(), monitoring.RecoverySuccess{Platform: "openai", Model: "gpt-5"})
	if service.queuedEvents.Load() != 1 {
		t.Fatalf("recovery success queued events = %d, want 1", service.queuedEvents.Load())
	}
}

func TestWorkerNotifyAndNotificationHelpers(t *testing.T) {
	repo := &monitorRepoStub{
		autoResolve: []int{workerBatchSize, 2},
		cleanup:     []int{workerBatchSize, 1},
		cleanupReq:  []int{3},
		notifyDue: []Event{{
			ID: 9, Severity: monitoring.SeverityCritical, Type: monitoring.TypePluginError, Status: monitoring.StatusActive,
			Source: monitoring.SourcePluginManager, SubjectType: monitoring.SubjectPlugin, SubjectID: "subject",
			Title: "Plugin failed", Message: "", PluginID: "gateway-openai", CreatedAt: time.Date(2026, 6, 20, 1, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 6, 20, 2, 0, 0, 0, time.UTC),
		}},
	}
	publisher := &monitorPublisherStub{}
	notifier := &monitorNotifierStub{configured: true}
	service := NewService(repo, WithNotifier(notifier), WithEventPublisher(publisher))

	service.runAutoResolveOnce(t.Context())
	service.runCleanupExpiredOnce(t.Context())
	service.runNotifyOnce(t.Context())

	if repo.autoResolveCalls != 2 || repo.cleanupCalls != 2 || repo.cleanupReqCalls != 1 {
		t.Fatalf("worker calls auto=%d cleanup=%d cleanupReq=%d", repo.autoResolveCalls, repo.cleanupCalls, repo.cleanupReqCalls)
	}
	if !containsReason(publisher.reasons, "auto_resolved") || !containsReason(publisher.reasons, "cleanup") {
		t.Fatalf("publisher reasons = %v", publisher.reasons)
	}
	if len(notifier.sent) != 1 || notifier.sent[0]["subject"] != "gateway-openai" || notifier.sent[0]["content"] != "Plugin failed" {
		t.Fatalf("notifier sent = %+v", notifier.sent)
	}
	if repo.markedNotifiedID != 9 {
		t.Fatalf("markedNotifiedID = %d, want 9", repo.markedNotifiedID)
	}
	if ok, token := service.claimNotify(t.Context(), 9); !ok || token != "" {
		t.Fatalf("claimNotify without redis = %v %q", ok, token)
	}
	service.releaseNotify(t.Context(), 9, "")
	if monitorNotifyLockKey(7) != "monitor:notify:7" {
		t.Fatalf("monitorNotifyLockKey = %q", monitorNotifyLockKey(7))
	}
	if notificationCooldown(monitoring.SeverityCritical) != 10*time.Minute || notificationCooldown(monitoring.SeverityWarning) != 30*time.Minute {
		t.Fatal("notificationCooldown mismatch")
	}
	if intToString(nil) != "" || timePtrToString(nil) != "" || timePtrToString(&time.Time{}) != "" {
		t.Fatal("nil/zero formatting helpers should return blank")
	}
}

type monitorRepoStub struct {
	inserted         [][]QueuedEvent
	insertedRequests [][]QueuedRequestEvent
	resolvedQueries  []monitoring.ResolveQuery
	resolvedID       int
	insertErr        error
	insertRequestErr error
	resolveErr       error
	resolveQueryErr  error
	insertCh         chan struct{}
	insertRequestCh  chan struct{}
	resolveCh        chan struct{}

	get                func(context.Context, int) (Event, error)
	list               func(context.Context, ListFilter) (ListResult, error)
	listRequests       func(context.Context, RequestListFilter) (RequestListResult, error)
	clearRequestEvents func(context.Context, *time.Time) (int, error)
	summary            func(context.Context) (Summary, error)
	requestSummary     func(context.Context) (Summary, error)

	autoResolve      []int
	autoResolveErr   error
	autoResolveCalls int
	cleanup          []int
	cleanupErr       error
	cleanupCalls     int
	cleanupReq       []int
	cleanupReqErr    error
	cleanupReqCalls  int
	notifyDue        []Event
	notifyErr        error
	markedNotifiedID int
	markNotifiedErr  error
	markFailedErr    error
}

func (m *monitorRepoStub) InsertBatch(_ context.Context, events []QueuedEvent) error {
	m.inserted = append(m.inserted, events)
	notifyTestChannel(m.insertCh)
	return m.insertErr
}

func (m *monitorRepoStub) InsertRequestBatch(_ context.Context, events []QueuedRequestEvent) error {
	m.insertedRequests = append(m.insertedRequests, events)
	notifyTestChannel(m.insertRequestCh)
	return m.insertRequestErr
}

func (m *monitorRepoStub) ResolveBySubject(_ context.Context, query monitoring.ResolveQuery) error {
	m.resolvedQueries = append(m.resolvedQueries, query)
	notifyTestChannel(m.resolveCh)
	return m.resolveQueryErr
}

func (m *monitorRepoStub) Get(ctx context.Context, id int) (Event, error) {
	if m.get != nil {
		return m.get(ctx, id)
	}
	return Event{}, ErrEventNotFound
}

func (m *monitorRepoStub) Resolve(_ context.Context, id int) error {
	m.resolvedID = id
	return m.resolveErr
}

func (m *monitorRepoStub) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	if m.list != nil {
		return m.list(ctx, filter)
	}
	return ListResult{}, nil
}

func (m *monitorRepoStub) ListRequests(ctx context.Context, filter RequestListFilter) (RequestListResult, error) {
	if m.listRequests != nil {
		return m.listRequests(ctx, filter)
	}
	return RequestListResult{}, nil
}

func (m *monitorRepoStub) ClearRequestEvents(ctx context.Context, before *time.Time) (int, error) {
	if m.clearRequestEvents != nil {
		return m.clearRequestEvents(ctx, before)
	}
	return 0, nil
}

func (m *monitorRepoStub) Summary(ctx context.Context) (Summary, error) {
	if m.summary != nil {
		return m.summary(ctx)
	}
	return Summary{}, nil
}

func (m *monitorRepoStub) RequestSummary(ctx context.Context) (Summary, error) {
	if m.requestSummary != nil {
		return m.requestSummary(ctx)
	}
	return Summary{}, nil
}

func (m *monitorRepoStub) CleanupExpired(context.Context, time.Time, int) (int, error) {
	m.cleanupCalls++
	if m.cleanupErr != nil {
		return 0, m.cleanupErr
	}
	return popInt(&m.cleanup), nil
}

func (m *monitorRepoStub) CleanupExpiredRequests(context.Context, time.Time, int) (int, error) {
	m.cleanupReqCalls++
	if m.cleanupReqErr != nil {
		return 0, m.cleanupReqErr
	}
	return popInt(&m.cleanupReq), nil
}

func (m *monitorRepoStub) AutoResolveDue(context.Context, time.Time, int) (int, error) {
	m.autoResolveCalls++
	if m.autoResolveErr != nil {
		return 0, m.autoResolveErr
	}
	return popInt(&m.autoResolve), nil
}

func (m *monitorRepoStub) ListNotifyDue(context.Context, time.Time, int) ([]Event, error) {
	if m.notifyErr != nil {
		return nil, m.notifyErr
	}
	return m.notifyDue, nil
}

func (m *monitorRepoStub) MarkNotified(_ context.Context, id int, _ time.Time, _ time.Time) error {
	m.markedNotifiedID = id
	return m.markNotifiedErr
}

func (m *monitorRepoStub) MarkNotifyFailed(context.Context, int, time.Time, string) error {
	return m.markFailedErr
}

type monitorPublisherStub struct {
	reasons []string
}

func (m *monitorPublisherStub) PublishMonitorChanged(reason string) {
	m.reasons = append(m.reasons, reason)
}

type monitorNotifierStub struct {
	configured bool
	sent       []map[string]string
	err        error
	configErr  error
}

func (m *monitorNotifierStub) IsConfigured(context.Context) (bool, error) {
	return m.configured, m.configErr
}

func (m *monitorNotifierStub) Send(_ context.Context, values map[string]string) error {
	m.sent = append(m.sent, values)
	return m.err
}

func popInt(values *[]int) int {
	if len(*values) == 0 {
		return 0
	}
	value := (*values)[0]
	*values = (*values)[1:]
	return value
}

func stringsOfLength(seed string, length int) string {
	out := ""
	for len([]rune(out)) < length {
		out += seed
	}
	return out
}

func containsReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func notifyTestChannel(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}
