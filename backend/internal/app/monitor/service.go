package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

const (
	maxTitleLength       = 160
	maxMessageLength     = 500
	maxCodeLength        = 64
	maxSourceLength      = 64
	maxSubjectTypeLength = 64
	maxSubjectIDLength   = 128
	maxPlatformLength    = 128
	maxEndpointLength    = 256
	maxRequestIDLength   = 128
	maxFingerprintLength = 128
	maxSnapshotLength    = 255
	maxDetailJSONBytes   = 4 * 1024
	maxDetailArrayItems  = 5

	dropLogInterval = time.Minute
)

var monitorNotifyUnlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

var (
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	skKeyPattern  = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
	emailPattern  = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	secretPattern = regexp.MustCompile(`(?i)\b(authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|token|secret|cookie|session)\b\s*[:=]\s*["']?[^"',\s}]+`)
)

// Option customizes the monitor service.
type Option func(*Service)

// Notifier is the existing notification service surface used by monitor alerts.
type Notifier interface {
	IsConfigured(context.Context) (bool, error)
	Send(context.Context, map[string]string) error
}

// EventPublisher receives best-effort monitor change notifications.
type EventPublisher interface {
	PublishMonitorChanged(reason string)
}

// Service provides best-effort temporary monitoring.
type Service struct {
	repo           Repository
	notifier       Notifier
	eventPublisher EventPublisher
	rdb            *redis.Client
	queue          chan queuedOperation
	recovery       *recoverySnapshot
	retention      time.Duration
	flushInterval  time.Duration
	flushBatchSize int

	droppedEvents      atomic.Int64
	queuedEvents       atomic.Int64
	flushedEvents      atomic.Int64
	aggregatorPanics   atomic.Int64
	workerPanics       atomic.Int64
	lastDropLogUnixSec atomic.Int64
}

var _ monitoring.Recorder = (*Service)(nil)
var _ monitoring.RecoveryRecorder = (*Service)(nil)

// NewService creates a monitor service. It is safe for hot-path callers because
// Record only normalizes input and performs a non-blocking queue send.
func NewService(repo Repository, opts ...Option) *Service {
	s := &Service{
		repo:           repo,
		queue:          make(chan queuedOperation, defaultQueueSize),
		recovery:       newRecoverySnapshot(),
		retention:      defaultRetention,
		flushInterval:  defaultFlushInterval,
		flushBatchSize: defaultFlushBatchSize,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if s.retention <= 0 {
		s.retention = defaultRetention
	}
	if s.flushInterval <= 0 {
		s.flushInterval = defaultFlushInterval
	}
	if s.flushBatchSize <= 0 {
		s.flushBatchSize = defaultFlushBatchSize
	}
	if s.queue == nil {
		s.queue = make(chan queuedOperation, defaultQueueSize)
	}
	return s
}

// WithQueueSize overrides the in-memory queue capacity.
func WithQueueSize(size int) Option {
	return func(s *Service) {
		if size > 0 {
			s.queue = make(chan queuedOperation, size)
		}
	}
}

// WithFlushBatchSize overrides the number of queued events that triggers a flush.
func WithFlushBatchSize(size int) Option {
	return func(s *Service) {
		if size > 0 {
			s.flushBatchSize = size
		}
	}
}

// WithFlushInterval overrides the periodic flush interval.
func WithFlushInterval(interval time.Duration) Option {
	return func(s *Service) {
		if interval > 0 {
			s.flushInterval = interval
		}
	}
}

// WithRetention overrides the event retention window.
func WithRetention(retention time.Duration) Option {
	return func(s *Service) {
		if retention > 0 {
			s.retention = retention
		}
	}
}

// WithNotifier enables monitor notifications through the existing notification service.
func WithNotifier(notifier Notifier) Option {
	return func(s *Service) {
		s.notifier = notifier
	}
}

// WithEventPublisher publishes monitor list/summary change notifications.
func WithEventPublisher(publisher EventPublisher) Option {
	return func(s *Service) {
		s.eventPublisher = publisher
	}
}

// WithRedis enables cross-instance monitor notification claiming.
func WithRedis(rdb *redis.Client) Option {
	return func(s *Service) {
		s.rdb = rdb
	}
}

// Record implements monitoring.Recorder. It intentionally never returns errors
// to callers on request-forwarding or scheduler paths.
func (s *Service) Record(ctx context.Context, input monitoring.EventInput) {
	if s == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	event := s.normalizeInput(input)
	select {
	case s.queue <- queuedOperation{Kind: queuedOperationRecord, Event: event}:
		s.rememberRecoveryEvent(event)
		s.queuedEvents.Add(1)
	default:
		dropped := s.droppedEvents.Add(1)
		s.logDrop(dropped)
	}
}

// RecordRequest implements requestmonitoring.Recorder. It is intentionally
// async and best-effort so request forwarding does not depend on monitor storage.
func (s *Service) RecordRequest(ctx context.Context, input requestmonitoring.EventInput) {
	if s == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	event := s.normalizeRequestInput(input)
	select {
	case s.queue <- queuedOperation{Kind: queuedOperationRecordRequest, RequestEvent: event}:
		s.queuedEvents.Add(1)
	default:
		dropped := s.droppedEvents.Add(1)
		s.logDrop(dropped)
	}
}

// ResolveBySubject schedules active events for a subject to be resolved. It is
// intentionally best-effort and never performs database work in the caller path.
func (s *Service) ResolveBySubject(ctx context.Context, query monitoring.ResolveQuery) {
	if s == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	select {
	case s.queue <- queuedOperation{Kind: queuedOperationResolve, Resolve: cloneResolveQuery(query)}:
		s.queuedEvents.Add(1)
	default:
		dropped := s.droppedEvents.Add(1)
		s.logDrop(dropped)
	}
}

// Get returns one monitor event by id.
func (s *Service) Get(ctx context.Context, id int) (Event, error) {
	if s == nil || s.repo == nil {
		return Event{}, ErrEventNotFound
	}
	return s.repo.Get(ctx, id)
}

// List returns a cursor page for future API handlers.
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	if s == nil || s.repo == nil {
		return ListResult{}, nil
	}
	filter.Limit = normalizeListLimit(filter.Limit)
	return s.repo.List(ctx, filter)
}

// ListRequests returns a cursor page of request monitor events.
func (s *Service) ListRequests(ctx context.Context, filter RequestListFilter) (RequestListResult, error) {
	if s == nil || s.repo == nil {
		return RequestListResult{}, nil
	}
	filter.Limit = normalizeListLimit(filter.Limit)
	return s.repo.ListRequests(ctx, filter)
}

// ClearRequestEvents deletes request monitor rows. A nil before value clears all rows.
func (s *Service) ClearRequestEvents(ctx context.Context, before *time.Time) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	deleted, err := s.repo.ClearRequestEvents(ctx, before)
	if err != nil {
		return 0, err
	}
	s.publishMonitorChanged("request_cleared")
	return deleted, nil
}

// Summary returns the monitor event overview for future dashboard handlers.
func (s *Service) Summary(ctx context.Context) (Summary, error) {
	if s == nil || s.repo == nil {
		return Summary{}, nil
	}
	return s.repo.Summary(ctx)
}

// RequestSummary returns request monitor event counts. Request events are
// append-only and do not have active/resolved state.
func (s *Service) RequestSummary(ctx context.Context) (Summary, error) {
	if s == nil || s.repo == nil {
		return Summary{}, nil
	}
	return s.repo.RequestSummary(ctx)
}

// RuntimeStats returns cheap in-memory counters for the runtime sampler.
func (s *Service) RuntimeStats() RuntimeStats {
	if s == nil {
		return RuntimeStats{}
	}
	return RuntimeStats{
		QueueLen:        len(s.queue),
		QueueCap:        cap(s.queue),
		QueuedTotal:     s.queuedEvents.Load(),
		FlushedTotal:    s.flushedEvents.Load(),
		DroppedTotal:    s.droppedEvents.Load(),
		AggregatorPanic: s.aggregatorPanics.Load(),
		WorkerPanic:     s.workerPanics.Load(),
	}
}

// Resolve marks one monitor event resolved.
func (s *Service) Resolve(ctx context.Context, id int) error {
	if s == nil || s.repo == nil {
		return ErrEventNotFound
	}
	event, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if nonManualRecoverableEvent(event) {
		return ErrEventNotRecoverable
	}
	if err := s.repo.Resolve(ctx, id); err != nil {
		return err
	}
	s.forgetRecoveryEvent(ctx, event)
	s.publishMonitorChanged("resolved")
	return nil
}

func (s *Service) publishMonitorChanged(reason string) {
	if s == nil || s.eventPublisher == nil {
		return
	}
	s.eventPublisher.PublishMonitorChanged(reason)
}

func (s *Service) normalizeInput(input monitoring.EventInput) QueuedEvent {
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	subjectType := truncateString(defaultString(input.SubjectType, monitoring.SubjectSystem), maxSubjectTypeLength)
	eventType := normalizeType(input.Type)
	severity := normalizeEventSeverity(eventType, subjectType, input.Severity)
	source := truncateString(defaultString(input.Source, monitoring.SourceMonitorWorker), maxSourceLength)
	subjectID := inferSubjectID(input, subjectType)
	detail := sanitizeDetail(input.Detail)

	title := scrubText(input.Title)
	if strings.TrimSpace(title) == "" {
		title = defaultTitle(eventType)
	}
	message := scrubText(input.Message)

	event := Event{
		Type:                eventType,
		Severity:            severity,
		Status:              monitoring.StatusActive,
		Source:              source,
		SubjectType:         subjectType,
		SubjectID:           subjectID,
		Title:               truncateString(title, maxTitleLength),
		Message:             truncateString(message, maxMessageLength),
		AccountID:           cloneIntPtr(input.AccountID),
		AccountNameSnapshot: truncateString(input.AccountNameSnapshot, maxSnapshotLength),
		Platform:            truncateString(input.Platform, maxPlatformLength),
		PluginID:            truncateString(input.PluginID, maxPlatformLength),
		TaskType:            truncateString(input.TaskType, maxPlatformLength),
		ErrorCode:           truncateString(input.ErrorCode, maxCodeLength),
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
		ExpiresAt:           observedAt.Add(s.retention),
		Detail:              detail,
	}
	event.Hash = hashFor(input.HashMaterial, event)
	event.RecoveryMode = recoveryModeForEvent(event)
	switch event.RecoveryMode {
	case monitoring.RecoveryModeNone, monitoring.RecoveryModeExternal:
		event.AutoResolveAt = nil
	case monitoring.RecoveryModeSuccess:
		event.AutoResolveAt = recoveryFallbackAt(input.AutoResolveAt, observedAt)
	case monitoring.RecoveryModeManual:
		event.AutoResolveAt = resolveAtFor(input.AutoResolveAt, event.Type, observedAt)
	}
	return QueuedEvent{Event: event}
}

func (s *Service) normalizeRequestInput(input requestmonitoring.EventInput) QueuedRequestEvent {
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	eventType := normalizeRequestType(input.Type)
	severity := normalizeRequestSeverity(input.Severity)
	source := truncateString(defaultString(input.Source, monitoring.SourceForwarder), maxSourceLength)
	detail := sanitizeDetail(detailWithRequestPath(input.Detail, input.RequestPath))

	title := scrubText(input.Title)
	if strings.TrimSpace(title) == "" {
		title = defaultRequestTitle(eventType)
	}
	message := scrubText(input.Message)

	event := RequestEvent{
		Type:                eventType,
		Severity:            severity,
		Source:              source,
		Fingerprint:         truncateString(input.Fingerprint, maxFingerprintLength),
		Title:               truncateString(title, maxTitleLength),
		Message:             truncateString(message, maxMessageLength),
		RequestID:           truncateString(input.RequestID, maxRequestIDLength),
		APIKeyID:            cloneIntPtr(input.APIKeyID),
		APIKeyNameSnapshot:  truncateString(input.APIKeyNameSnapshot, maxSnapshotLength),
		UserID:              cloneIntPtr(input.UserID),
		UserEmailSnapshot:   truncateString(input.UserEmailSnapshot, maxSnapshotLength),
		GroupID:             cloneIntPtr(input.GroupID),
		AccountID:           cloneIntPtr(input.AccountID),
		AccountNameSnapshot: truncateString(input.AccountNameSnapshot, maxSnapshotLength),
		Platform:            truncateString(input.Platform, maxPlatformLength),
		PluginID:            truncateString(input.PluginID, maxPlatformLength),
		Method:              truncateString(strings.ToUpper(strings.TrimSpace(input.Method)), maxCodeLength),
		Endpoint:            truncateString(input.Endpoint, maxEndpointLength),
		Model:               truncateString(input.Model, maxPlatformLength),
		HTTPStatus:          cloneIntPtr(input.HTTPStatus),
		UpstreamStatus:      cloneIntPtr(input.UpstreamStatus),
		ErrorCode:           truncateString(input.ErrorCode, maxCodeLength),
		DurationMS:          maxInt64(input.DurationMS, 0),
		CreatedAt:           observedAt,
		ExpiresAt:           observedAt.Add(s.retention),
		Detail:              detail,
	}
	event.Hash = requestHashFor(input.HashMaterial, event)
	return QueuedRequestEvent{RequestEvent: event}
}

func (s *Service) logDrop(total int64) {
	now := time.Now().Unix()
	last := s.lastDropLogUnixSec.Load()
	if now-last < int64(dropLogInterval/time.Second) {
		return
	}
	if s.lastDropLogUnixSec.CompareAndSwap(last, now) {
		slog.Warn("monitor_event_queue_full", "dropped_total", total, "queue_len", len(s.queue), "queue_cap", cap(s.queue))
	}
}

func normalizeListLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func normalizeType(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case monitoring.TypeSchedulerError:
		return monitoring.TypeSchedulerError
	case monitoring.TypeUpstreamAccountError:
		return monitoring.TypeUpstreamAccountError
	case monitoring.TypePluginError:
		return monitoring.TypePluginError
	case monitoring.TypeTaskError:
		return monitoring.TypeTaskError
	case monitoring.TypeSystemError:
		return monitoring.TypeSystemError
	default:
		return monitoring.TypeSystemError
	}
}

func normalizeRequestType(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case requestmonitoring.TypeAPIRequestError:
		return requestmonitoring.TypeAPIRequestError
	case requestmonitoring.TypePluginRouteError:
		return requestmonitoring.TypePluginRouteError
	case requestmonitoring.TypePluginForwardError:
		return requestmonitoring.TypePluginForwardError
	case requestmonitoring.TypeClientRequestError:
		return requestmonitoring.TypeClientRequestError
	case requestmonitoring.TypeClientClosed:
		return requestmonitoring.TypeClientClosed
	default:
		return requestmonitoring.TypeAPIRequestError
	}
}

func normalizeSeverity(severity string) string {
	switch strings.TrimSpace(severity) {
	case monitoring.SeverityInfo:
		return monitoring.SeverityInfo
	case monitoring.SeverityCritical:
		return monitoring.SeverityCritical
	case monitoring.SeverityError:
		return monitoring.SeverityError
	case monitoring.SeverityWarning:
		return monitoring.SeverityWarning
	default:
		return monitoring.SeverityWarning
	}
}

func normalizeEventSeverity(eventType string, subjectType string, severity string) string {
	normalized := normalizeSeverity(severity)
	if eventType == monitoring.TypeUpstreamAccountError && subjectType == monitoring.SubjectAccount {
		return monitoring.SeverityWarning
	}
	return normalized
}

func normalizeRequestSeverity(severity string) string {
	switch strings.TrimSpace(severity) {
	case requestmonitoring.SeverityWarning:
		return requestmonitoring.SeverityWarning
	case requestmonitoring.SeverityInfo:
		return requestmonitoring.SeverityInfo
	default:
		return requestmonitoring.SeverityInfo
	}
}

func inferSubjectID(input monitoring.EventInput, subjectType string) string {
	if input.SubjectID != "" {
		return truncateString(input.SubjectID, maxSubjectIDLength)
	}
	switch subjectType {
	case monitoring.SubjectAccount:
		if input.AccountID != nil {
			return strconv.Itoa(*input.AccountID)
		}
	case monitoring.SubjectPlugin:
		if input.PluginID != "" {
			return truncateString(input.PluginID, maxSubjectIDLength)
		}
	case monitoring.SubjectTask:
		if input.TaskType != "" {
			return truncateString(input.TaskType, maxSubjectIDLength)
		}
	}
	return ""
}

func hashFor(material string, event Event) string {
	if strings.TrimSpace(material) == "" {
		material = defaultHashMaterial(event)
	}
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

func defaultHashMaterial(event Event) string {
	switch event.Type {
	case monitoring.TypeUpstreamAccountError:
		return joinHashParts(event.Type, intPtrValue(event.AccountID), event.ErrorCode)
	case monitoring.TypeSchedulerError:
		return joinHashParts(event.Type, event.Platform, event.SubjectID, event.ErrorCode, monitorDetailString(event.Detail, "model"))
	case monitoring.TypePluginError:
		return joinHashParts(event.Type, event.PluginID, event.ErrorCode)
	case monitoring.TypeTaskError:
		return joinHashParts(event.Type, event.PluginID, event.TaskType, event.ErrorCode)
	default:
		return joinHashParts(event.Type, event.Source, event.SubjectType, event.SubjectID, event.ErrorCode)
	}
}

func requestHashFor(material string, event RequestEvent) string {
	if strings.TrimSpace(material) == "" {
		material = defaultRequestHashMaterial(event)
	}
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

func defaultRequestHashMaterial(event RequestEvent) string {
	switch event.Type {
	case requestmonitoring.TypePluginForwardError:
		return joinHashParts(event.Type, event.PluginID, intPtrValue(event.AccountID), event.Endpoint, event.ErrorCode)
	case requestmonitoring.TypeClientClosed:
		return joinHashParts(event.Type, intPtrValue(event.APIKeyID), event.Method, event.Endpoint)
	default:
		return joinHashParts(event.Type, intPtrValue(event.APIKeyID), event.Method, event.Endpoint, event.ErrorCode, intPtrValue(event.HTTPStatus))
	}
}

func joinHashParts(parts ...string) string {
	return strings.Join(parts, "\x1f")
}

func resolveAtFor(input *time.Time, eventType string, observedAt time.Time) *time.Time {
	if input != nil && !input.IsZero() {
		t := *input
		return &t
	}
	t := observedAt.Add(autoResolveWindow(eventType))
	return &t
}

func autoResolveWindow(eventType string) time.Duration {
	switch eventType {
	case monitoring.TypeSchedulerError:
		return 15 * time.Minute
	case monitoring.TypeUpstreamAccountError:
		return time.Hour
	case monitoring.TypePluginError:
		return 30 * time.Minute
	case monitoring.TypeTaskError:
		return 2 * time.Hour
	default:
		return time.Hour
	}
}

func defaultTitle(eventType string) string {
	switch eventType {
	case monitoring.TypeSchedulerError:
		return "Scheduler error"
	case monitoring.TypeUpstreamAccountError:
		return "Upstream account error"
	case monitoring.TypePluginError:
		return "Plugin error"
	case monitoring.TypeTaskError:
		return "Task error"
	default:
		return "System monitor event"
	}
}

func defaultRequestTitle(eventType string) string {
	switch eventType {
	case requestmonitoring.TypePluginRouteError:
		return "Plugin route error"
	case requestmonitoring.TypePluginForwardError:
		return "Plugin forward error"
	case requestmonitoring.TypeClientRequestError:
		return "Client request error"
	case requestmonitoring.TypeClientClosed:
		return "Client closed request"
	default:
		return "API request error"
	}
}

func scrubText(text string) string {
	text = bearerPattern.ReplaceAllString(text, "Bearer [REDACTED]")
	text = skKeyPattern.ReplaceAllString(text, "sk-[REDACTED]")
	text = secretPattern.ReplaceAllString(text, "$1=[REDACTED]")
	text = emailPattern.ReplaceAllString(text, "[REDACTED_EMAIL]")
	return text
}

func sanitizeDetail(detail map[string]interface{}) map[string]interface{} {
	if len(detail) == 0 {
		return map[string]interface{}{}
	}
	out := sanitizeMap(detail, 0)
	if detailFits(out) {
		return out
	}
	trimmed := make(map[string]interface{}, len(out)+1)
	keys := make([]string, 0, len(out))
	for key := range out {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		trimmed[key] = compactDetailValue(out[key])
		if !detailFits(trimmed) {
			delete(trimmed, key)
			break
		}
	}
	trimmed["_truncated"] = true
	if detailFits(trimmed) {
		return trimmed
	}
	return map[string]interface{}{"_truncated": true}
}

func detailWithRequestPath(detail map[string]interface{}, requestPath string) map[string]interface{} {
	requestPath = truncateString(requestPath, maxEndpointLength)
	if requestPath == "" {
		return detail
	}
	out := make(map[string]interface{}, len(detail)+1)
	for key, value := range detail {
		out[key] = value
	}
	out["request_path"] = requestPath
	return out
}

func sanitizeMap(in map[string]interface{}, depth int) map[string]interface{} {
	if depth > 3 {
		return map[string]interface{}{"_truncated": true}
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		key = truncateString(key, maxCodeLength)
		if isSensitiveKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = sanitizeValue(value, depth+1)
	}
	return out
}

func sanitizeValue(value interface{}, depth int) interface{} {
	switch v := value.(type) {
	case nil, bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return v
	case string:
		return truncateString(scrubText(v), maxMessageLength)
	case []string:
		return sanitizeStringSlice(v)
	case []interface{}:
		return sanitizeInterfaceSlice(v, depth)
	case map[string]interface{}:
		return sanitizeMap(v, depth)
	default:
		return truncateString(scrubText(fmt.Sprint(v)), maxMessageLength)
	}
}

func sanitizeStringSlice(values []string) []interface{} {
	out := make([]interface{}, 0, minInt(len(values), maxDetailArrayItems))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = truncateString(scrubText(value), maxMessageLength)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) >= maxDetailArrayItems {
			break
		}
	}
	return out
}

func sanitizeInterfaceSlice(values []interface{}, depth int) []interface{} {
	out := make([]interface{}, 0, minInt(len(values), maxDetailArrayItems))
	seenStrings := make(map[string]struct{}, len(values))
	for _, value := range values {
		sanitized := sanitizeValue(value, depth+1)
		if s, ok := sanitized.(string); ok {
			if _, seen := seenStrings[s]; seen {
				continue
			}
			seenStrings[s] = struct{}{}
		}
		out = append(out, sanitized)
		if len(out) >= maxDetailArrayItems {
			break
		}
	}
	return out
}

func compactDetailValue(value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		return truncateString(v, 120)
	case []interface{}:
		if len(v) > 2 {
			v = v[:2]
		}
		return v
	case map[string]interface{}:
		return map[string]interface{}{"_truncated": true}
	default:
		return v
	}
}

func detailFits(detail map[string]interface{}) bool {
	raw, err := json.Marshal(detail)
	return err == nil && len(raw) <= maxDetailJSONBytes
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "authorization") ||
		strings.Contains(k, "api_key") ||
		strings.Contains(k, "apikey") ||
		strings.Contains(k, "access_token") ||
		strings.Contains(k, "refresh_token") ||
		strings.Contains(k, "id_token") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "cookie") ||
		strings.Contains(k, "session")
}

func truncateString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func cloneResolveQuery(query monitoring.ResolveQuery) monitoring.ResolveQuery {
	query.AccountID = cloneIntPtr(query.AccountID)
	return query
}

func intPtrValue(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
