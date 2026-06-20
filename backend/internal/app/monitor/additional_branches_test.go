package monitor

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

func TestAdditionalNormalizeAndSanitizeBranches(t *testing.T) {
	StartAggregatorLoop(t.Context(), nil)
	var nilService *Service
	nilService.StartAggregatorLoop(t.Context())
	StartWorkerLoop(t.Context(), nil)
	nilService.StartWorkerLoop(t.Context())

	for _, item := range []struct {
		input string
		want  string
	}{
		{monitoring.TypeSchedulerError, monitoring.TypeSchedulerError},
		{monitoring.TypeUpstreamAccountError, monitoring.TypeUpstreamAccountError},
		{monitoring.TypePluginError, monitoring.TypePluginError},
		{monitoring.TypeTaskError, monitoring.TypeTaskError},
		{monitoring.TypeSystemError, monitoring.TypeSystemError},
		{"unknown", monitoring.TypeSystemError},
	} {
		if got := normalizeType(item.input); got != item.want {
			t.Fatalf("normalizeType(%q) = %q, want %q", item.input, got, item.want)
		}
	}
	for _, item := range []struct {
		input string
		want  string
	}{
		{requestmonitoring.TypeAPIRequestError, requestmonitoring.TypeAPIRequestError},
		{requestmonitoring.TypePluginRouteError, requestmonitoring.TypePluginRouteError},
		{requestmonitoring.TypePluginForwardError, requestmonitoring.TypePluginForwardError},
		{requestmonitoring.TypeClientRequestError, requestmonitoring.TypeClientRequestError},
		{requestmonitoring.TypeClientClosed, requestmonitoring.TypeClientClosed},
		{"unknown", requestmonitoring.TypeAPIRequestError},
	} {
		if got := normalizeRequestType(item.input); got != item.want {
			t.Fatalf("normalizeRequestType(%q) = %q, want %q", item.input, got, item.want)
		}
	}
	for _, severity := range []string{monitoring.SeverityInfo, monitoring.SeverityCritical, monitoring.SeverityError, monitoring.SeverityWarning} {
		if got := normalizeSeverity(severity); got != severity {
			t.Fatalf("normalizeSeverity(%q) = %q", severity, got)
		}
	}
	if got := normalizeSeverity("bad"); got != monitoring.SeverityWarning {
		t.Fatalf("normalizeSeverity bad = %q", got)
	}
	if normalizeRequestSeverity(requestmonitoring.SeverityWarning) != requestmonitoring.SeverityWarning ||
		normalizeRequestSeverity(requestmonitoring.SeverityInfo) != requestmonitoring.SeverityInfo ||
		normalizeRequestSeverity("bad") != requestmonitoring.SeverityInfo {
		t.Fatal("normalizeRequestSeverity returned unexpected values")
	}

	accountID := 17
	if got := inferSubjectID(monitoring.EventInput{AccountID: &accountID}, monitoring.SubjectAccount); got != "17" {
		t.Fatalf("account subject id = %q", got)
	}
	if got := inferSubjectID(monitoring.EventInput{PluginID: "gateway-openai"}, monitoring.SubjectPlugin); got != "gateway-openai" {
		t.Fatalf("plugin subject id = %q", got)
	}
	if got := inferSubjectID(monitoring.EventInput{TaskType: "refresh"}, monitoring.SubjectTask); got != "refresh" {
		t.Fatalf("task subject id = %q", got)
	}
	if got := inferSubjectID(monitoring.EventInput{}, monitoring.SubjectSystem); got != "" {
		t.Fatalf("system subject id = %q", got)
	}

	for _, eventType := range []string{
		monitoring.TypeSchedulerError,
		monitoring.TypeUpstreamAccountError,
		monitoring.TypePluginError,
		monitoring.TypeTaskError,
		monitoring.TypeSystemError,
	} {
		if defaultTitle(eventType) == "" || autoResolveWindow(eventType) <= 0 {
			t.Fatalf("default title/window missing for %s", eventType)
		}
	}
	for _, eventType := range []string{
		requestmonitoring.TypePluginRouteError,
		requestmonitoring.TypePluginForwardError,
		requestmonitoring.TypeClientRequestError,
		requestmonitoring.TypeClientClosed,
		requestmonitoring.TypeAPIRequestError,
	} {
		if defaultRequestTitle(eventType) == "" {
			t.Fatalf("default request title missing for %s", eventType)
		}
	}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	provided := now.Add(time.Minute)
	if got := resolveAtFor(&provided, monitoring.TypeTaskError, now); got == nil || !got.Equal(provided) {
		t.Fatalf("resolveAtFor provided = %v", got)
	}

	if got := defaultHashMaterial(Event{Type: monitoring.TypePluginError, PluginID: "p", ErrorCode: "e"}); !strings.Contains(got, "p") {
		t.Fatalf("plugin hash material = %q", got)
	}
	if got := defaultHashMaterial(Event{Type: monitoring.TypeTaskError, PluginID: "p", TaskType: "t", ErrorCode: "e"}); !strings.Contains(got, "t") {
		t.Fatalf("task hash material = %q", got)
	}
	if got := defaultRequestHashMaterial(RequestEvent{Type: requestmonitoring.TypePluginForwardError, PluginID: "p", Endpoint: "/v1", ErrorCode: "e"}); !strings.Contains(got, "p") {
		t.Fatalf("plugin forward request hash material = %q", got)
	}
	if got := defaultRequestHashMaterial(RequestEvent{Type: requestmonitoring.TypeAPIRequestError, Method: "POST", Endpoint: "/v1", ErrorCode: "e"}); !strings.Contains(got, "POST") {
		t.Fatalf("default request hash material = %q", got)
	}

	stringSlice := sanitizeValue([]string{"a", "a", "b", "c", "d", "e", "f"}, 0)
	values, ok := stringSlice.([]interface{})
	if !ok || len(values) != maxDetailArrayItems {
		t.Fatalf("sanitize string slice = %#v", stringSlice)
	}
	huge := make(map[string]interface{})
	for i := 0; i < 20; i++ {
		huge[strconv.Itoa(i)] = strings.Repeat("x", 700)
	}
	huge["list"] = []interface{}{
		strings.Repeat("y", 200),
		strings.Repeat("z", 200),
		strings.Repeat("w", 200),
	}
	huge["map"] = map[string]interface{}{"nested": strings.Repeat("m", 200)}
	sanitized := sanitizeDetail(huge)
	if sanitized["_truncated"] != true {
		t.Fatalf("large detail not marked truncated: %+v", sanitized)
	}
	if compactDetailValue(strings.Repeat("x", 200)).(string) != strings.Repeat("x", 120) {
		t.Fatal("compactDetailValue string did not truncate to 120 runes")
	}
	if got := compactDetailValue([]interface{}{1, 2, 3}).([]interface{}); len(got) != 2 {
		t.Fatalf("compactDetailValue slice len = %d, want 2", len(got))
	}
	if got := compactDetailValue(map[string]interface{}{"a": 1}).(map[string]interface{}); got["_truncated"] != true {
		t.Fatalf("compactDetailValue map = %+v", got)
	}
	if minInt(1, 2) != 1 || minInt(3, 2) != 2 || maxInt64(3, 2) != 3 || maxInt64(1, 2) != 2 {
		t.Fatal("min/max helpers returned unexpected values")
	}
}

func TestAdditionalQueueRecoveryAndRedisBranches(t *testing.T) {
	service := NewService(nil, WithQueueSize(1))
	service.RecordRequest(t.Context(), requestmonitoring.EventInput{Type: requestmonitoring.TypeAPIRequestError})
	if service.queuedEvents.Load() != 1 {
		t.Fatalf("RecordRequest queued = %d, want 1", service.queuedEvents.Load())
	}
	service.RecordRequest(t.Context(), requestmonitoring.EventInput{Type: requestmonitoring.TypeAPIRequestError})
	if service.droppedEvents.Load() != 1 {
		t.Fatalf("RecordRequest dropped = %d, want 1", service.droppedEvents.Load())
	}
	service.ResolveBySubject(t.Context(), monitoring.ResolveQuery{Hash: "queued"})
	if service.droppedEvents.Load() != 2 {
		t.Fatalf("ResolveBySubject on full queue dropped = %d, want 2", service.droppedEvents.Load())
	}
	service2 := NewService(nil, WithQueueSize(1))
	service2.ResolveBySubject(t.Context(), monitoring.ResolveQuery{Hash: "queued"})
	if service2.queuedEvents.Load() != 1 {
		t.Fatalf("ResolveBySubject queued = %d, want 1", service2.queuedEvents.Load())
	}

	var nilService *Service
	nilService.RecordRequest(t.Context(), requestmonitoring.EventInput{})
	nilService.ResolveBySubject(t.Context(), monitoring.ResolveQuery{})
	nilService.RecordRecoverySuccess(t.Context(), monitoring.RecoverySuccess{})

	recovery := newRecoverySnapshot()
	recovery.remember("", time.Time{})
	recovery.forget("")
	recovery.remember("zero-expiry", time.Time{})
	if !recovery.has("zero-expiry", time.Now()) {
		t.Fatal("zero-expiry recovery entry should be active")
	}
	if recovery.snapshot().entries["zero-expiry"].IsZero() {
		t.Fatal("zero-expiry recovery entry should receive default expiry")
	}
	emptySnapshot := (&recoverySnapshot{}).snapshot()
	if emptySnapshot.entries == nil || emptySnapshot.active {
		t.Fatalf("empty snapshot = %+v", emptySnapshot)
	}

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	autoResolve := now.Add(30 * time.Minute)
	if !recoveryKeyExpiresAt(Event{AutoResolveAt: &autoResolve, ExpiresAt: now.Add(time.Hour)}).Equal(autoResolve) {
		t.Fatal("recoveryKeyExpiresAt should prefer AutoResolveAt")
	}
	if recoveryKeyExpiresAt(Event{}).Before(time.Now()) {
		t.Fatal("recoveryKeyExpiresAt fallback should be in the future")
	}
	if got := recoveryFallbackAt(&autoResolve, now); got == nil || !got.Equal(autoResolve) {
		t.Fatalf("recoveryFallbackAt provided = %v", got)
	}
	if hash, ok := recoveryHashForEvent(Event{Type: monitoring.TypeSchedulerError, SubjectType: monitoring.SubjectScheduler, ErrorCode: "other", Detail: map[string]interface{}{"model": "gpt"}}); ok || hash != "" {
		t.Fatalf("recoveryHashForEvent unsupported code = %q %v", hash, ok)
	}
	if hash, ok := recoveryHashForEvent(Event{Type: monitoring.TypeSchedulerError, SubjectType: monitoring.SubjectScheduler, ErrorCode: "no_available_account"}); ok || hash != "" {
		t.Fatalf("recoveryHashForEvent missing model = %q %v", hash, ok)
	}
	if recoveryModeForEvent(Event{Severity: monitoring.SeverityWarning}) != monitoring.RecoveryModeNone {
		t.Fatal("warning event should not be recoverable")
	}
	if recoveryModeForEvent(Event{Severity: monitoring.SeverityError, Type: monitoring.TypeUpstreamAccountError, ErrorCode: "account_dead"}) != monitoring.RecoveryModeExternal {
		t.Fatal("account_dead should use external recovery")
	}
	if recoveryResolveQueries(monitoring.RecoverySuccess{}) != nil {
		t.Fatal("empty recovery success should not produce queries")
	}
	if event := recoveryEventFromSuccess(monitoring.RecoverySuccess{GroupID: 5, Model: "gpt-5"}); event.SubjectID != "5" {
		t.Fatalf("recovery event subject = %q, want group id", event.SubjectID)
	}
	if monitorDetailString(map[string]interface{}{"fraction": 1.5}, "fraction") != "" {
		t.Fatal("non-integer float should not be converted to monitor detail string")
	}

	rdb, mock := redismock.NewClientMock()
	redisService := NewService(nil, WithRedis(rdb))
	recoveryEvent := QueuedEvent{Event: Event{
		Type:         monitoring.TypeSchedulerError,
		Severity:     monitoring.SeverityError,
		SubjectType:  monitoring.SubjectScheduler,
		SubjectID:    "openai",
		Hash:         "recovery-hash",
		ErrorCode:    "no_available_account",
		RecoveryMode: monitoring.RecoveryModeSuccess,
		AutoResolveAt: func() *time.Time {
			v := now.Add(time.Hour)
			return &v
		}(),
		Detail: map[string]interface{}{"model": "gpt-5"},
	}}
	mock.ExpectZAdd(monitorRecoveryZSetKey, redis.Z{
		Score:  float64(now.Add(time.Hour).Unix()),
		Member: "recovery-hash",
	}).SetVal(1)
	redisService.persistRecoveryEvents(t.Context(), []QueuedEvent{recoveryEvent})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("persist recovery expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	redisService = NewService(nil, WithRedis(rdb))
	mock.Regexp().ExpectSetNX(monitorNotifyLockKey(12), ".+", notifyLockTTL).SetVal(true)
	claimed, token := redisService.claimNotify(t.Context(), 12)
	if !claimed || token == "" {
		t.Fatalf("claimNotify = %v %q, want claimed token", claimed, token)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("claim notify expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	redisService = NewService(nil, WithRedis(rdb))
	mock.Regexp().ExpectSetNX(monitorNotifyLockKey(13), ".+", notifyLockTTL).SetErr(errors.New("redis down"))
	claimed, token = redisService.claimNotify(t.Context(), 13)
	if claimed || token != "" {
		t.Fatalf("claimNotify error = %v %q, want false blank", claimed, token)
	}
}

func TestAdditionalWorkerErrorBranches(t *testing.T) {
	repo := &monitorRepoStub{autoResolveErr: errors.New("auto failed")}
	service := NewService(repo, WithQueueSize(1))
	service.runAutoResolveOnce(t.Context())
	if service.queuedEvents.Load() != 1 {
		t.Fatalf("auto resolve error should queue monitor event, got %d", service.queuedEvents.Load())
	}

	repo = &monitorRepoStub{cleanupErr: errors.New("cleanup failed")}
	service = NewService(repo, WithQueueSize(1))
	service.runCleanupExpiredOnce(t.Context())
	if service.queuedEvents.Load() != 1 {
		t.Fatalf("cleanup error should queue monitor event, got %d", service.queuedEvents.Load())
	}

	repo = &monitorRepoStub{cleanup: []int{0}, cleanupReqErr: errors.New("request cleanup failed")}
	service = NewService(repo, WithQueueSize(1))
	service.runCleanupExpiredOnce(t.Context())
	if service.queuedEvents.Load() != 1 {
		t.Fatalf("request cleanup error should queue monitor event, got %d", service.queuedEvents.Load())
	}

	service = NewService(&monitorRepoStub{}, WithQueueSize(1))
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	service.runAutoResolveOnce(canceled)
	service.runCleanupExpiredOnce(canceled)

	notifier := &monitorNotifierStub{configured: false}
	service = NewService(&monitorRepoStub{}, WithNotifier(notifier))
	service.runNotifyOnce(t.Context())

	notifier = &monitorNotifierStub{configured: true, err: errors.New("send failed")}
	repo = &monitorRepoStub{notifyDue: []Event{{ID: 21, Severity: monitoring.SeverityError, Title: "failed notify"}}}
	service = NewService(repo, WithNotifier(notifier))
	service.runNotifyOnce(t.Context())
	if repo.markedNotifiedID != 0 {
		t.Fatalf("failed notify should not mark notified, got %d", repo.markedNotifiedID)
	}

	accountID := 77
	last := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	next := last.Add(time.Hour)
	values := monitorNotificationValues(Event{
		ID: 22, Severity: monitoring.SeverityWarning, Title: "Warning", Message: "message",
		SubjectID: "subject", AccountID: &accountID, AccountNameSnapshot: "Account 77",
		LastNotifiedAt: &last, NextNotifyAt: &next,
	})
	if values["subject"] != "Account 77" || values["account_id"] != strconv.Itoa(accountID) ||
		values["last_notified_at"] == "" || values["next_notify_at"] == "" {
		t.Fatalf("notification values = %+v", values)
	}
}
