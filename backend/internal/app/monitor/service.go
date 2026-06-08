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

// Service provides best-effort temporary monitoring.
type Service struct {
	repo           Repository
	notifier       Notifier
	rdb            *redis.Client
	queue          chan QueuedEvent
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

// NewService creates a monitor service. It is safe for hot-path callers because
// Record only normalizes input and performs a non-blocking queue send.
func NewService(repo Repository, opts ...Option) *Service {
	s := &Service{
		repo:           repo,
		queue:          make(chan QueuedEvent, defaultQueueSize),
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
		s.queue = make(chan QueuedEvent, defaultQueueSize)
	}
	return s
}

// WithQueueSize overrides the in-memory queue capacity.
func WithQueueSize(size int) Option {
	return func(s *Service) {
		if size > 0 {
			s.queue = make(chan QueuedEvent, size)
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
	case s.queue <- event:
		s.queuedEvents.Add(1)
	default:
		dropped := s.droppedEvents.Add(1)
		s.logDrop(dropped)
	}
}

// ResolveBySubject marks active events for a subject as resolved. Errors are
// logged and intentionally hidden from callers.
func (s *Service) ResolveBySubject(ctx context.Context, query monitoring.ResolveQuery) {
	if s == nil || s.repo == nil {
		return
	}
	if err := s.repo.ResolveBySubject(ctx, query); err != nil {
		slog.Warn("monitor_resolve_by_subject_failed", "error", err)
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

// Summary returns the active event overview for future dashboard handlers.
func (s *Service) Summary(ctx context.Context) (Summary, error) {
	if s == nil || s.repo == nil {
		return Summary{}, nil
	}
	return s.repo.Summary(ctx)
}

// Resolve marks one monitor event resolved.
func (s *Service) Resolve(ctx context.Context, id int) error {
	if s == nil || s.repo == nil {
		return ErrEventNotFound
	}
	return s.repo.Resolve(ctx, id)
}

// Ignore marks one monitor event ignored.
func (s *Service) Ignore(ctx context.Context, id int) error {
	if s == nil || s.repo == nil {
		return ErrEventNotFound
	}
	return s.repo.Ignore(ctx, id)
}

func (s *Service) normalizeInput(input monitoring.EventInput) QueuedEvent {
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	kind := normalizeKind(input.Kind)
	severity := normalizeSeverity(input.Severity)
	subjectType := truncateString(defaultString(input.SubjectType, monitoring.SubjectSystem), maxSubjectTypeLength)
	source := truncateString(defaultString(input.Source, monitoring.SourceMonitorWorker), maxSourceLength)
	subjectID := inferSubjectID(input, subjectType)
	detail := sanitizeDetail(detailWithRequestPath(input.Detail, input.RequestPath))

	title := scrubText(input.Title)
	if strings.TrimSpace(title) == "" {
		title = defaultTitle(kind)
	}
	message := scrubText(input.Message)

	event := Event{
		Kind:                kind,
		Severity:            severity,
		Status:              monitoring.StatusActive,
		Source:              source,
		SubjectType:         subjectType,
		SubjectID:           subjectID,
		Title:               truncateString(title, maxTitleLength),
		Message:             truncateString(message, maxMessageLength),
		APIKeyID:            cloneIntPtr(input.APIKeyID),
		APIKeyNameSnapshot:  truncateString(input.APIKeyNameSnapshot, maxSnapshotLength),
		UserID:              cloneIntPtr(input.UserID),
		UserEmailSnapshot:   truncateString(input.UserEmailSnapshot, maxSnapshotLength),
		GroupID:             cloneIntPtr(input.GroupID),
		AccountID:           cloneIntPtr(input.AccountID),
		AccountNameSnapshot: truncateString(input.AccountNameSnapshot, maxSnapshotLength),
		Platform:            truncateString(input.Platform, maxPlatformLength),
		PluginID:            truncateString(input.PluginID, maxPlatformLength),
		TaskType:            truncateString(input.TaskType, maxPlatformLength),
		Method:              truncateString(strings.ToUpper(strings.TrimSpace(input.Method)), maxCodeLength),
		Endpoint:            truncateString(input.Endpoint, maxEndpointLength),
		Model:               truncateString(input.Model, maxPlatformLength),
		HTTPStatus:          cloneIntPtr(input.HTTPStatus),
		UpstreamStatus:      cloneIntPtr(input.UpstreamStatus),
		ErrorCode:           truncateString(input.ErrorCode, maxCodeLength),
		ErrorType:           truncateString(input.ErrorType, maxCodeLength),
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
		ExpiresAt:           observedAt.Add(s.retention),
		Detail:              detail,
	}
	event.Fingerprint = fingerprintFor(input.FingerprintMaterial, event)
	event.AutoResolveAt = resolveAtFor(input.AutoResolveAt, event.Kind, observedAt)
	return QueuedEvent{Event: event}
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

func normalizeKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case monitoring.KindAPIRequestError:
		return monitoring.KindAPIRequestError
	case monitoring.KindSchedulerError:
		return monitoring.KindSchedulerError
	case monitoring.KindUpstreamAccountError:
		return monitoring.KindUpstreamAccountError
	case monitoring.KindPluginError:
		return monitoring.KindPluginError
	case monitoring.KindTaskError:
		return monitoring.KindTaskError
	case monitoring.KindSystemError:
		return monitoring.KindSystemError
	default:
		return monitoring.KindSystemError
	}
}

func normalizeSeverity(severity string) string {
	switch strings.TrimSpace(severity) {
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

func inferSubjectID(input monitoring.EventInput, subjectType string) string {
	if input.SubjectID != "" {
		return truncateString(input.SubjectID, maxSubjectIDLength)
	}
	switch subjectType {
	case monitoring.SubjectAPIKey:
		if input.APIKeyID != nil {
			return strconv.Itoa(*input.APIKeyID)
		}
	case monitoring.SubjectAccount:
		if input.AccountID != nil {
			return strconv.Itoa(*input.AccountID)
		}
	case monitoring.SubjectUser:
		if input.UserID != nil {
			return strconv.Itoa(*input.UserID)
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

func fingerprintFor(material string, event Event) string {
	if strings.TrimSpace(material) == "" {
		material = defaultFingerprintMaterial(event)
	}
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}

func defaultFingerprintMaterial(event Event) string {
	switch event.Kind {
	case monitoring.KindAPIRequestError:
		return joinFingerprintParts(event.Kind, intPtrValue(event.APIKeyID), event.Method, event.Endpoint, event.ErrorCode)
	case monitoring.KindUpstreamAccountError:
		return joinFingerprintParts(event.Kind, intPtrValue(event.AccountID), event.ErrorCode)
	case monitoring.KindSchedulerError:
		return joinFingerprintParts(event.Kind, event.Platform, event.Model, intPtrValue(event.GroupID), event.ErrorCode)
	case monitoring.KindPluginError:
		return joinFingerprintParts(event.Kind, event.PluginID, event.Endpoint, event.ErrorCode)
	case monitoring.KindTaskError:
		return joinFingerprintParts(event.Kind, event.PluginID, event.TaskType, event.ErrorCode)
	default:
		return joinFingerprintParts(event.Kind, event.Source, event.SubjectType, event.SubjectID, event.ErrorCode)
	}
}

func joinFingerprintParts(parts ...string) string {
	return strings.Join(parts, "\x1f")
}

func resolveAtFor(input *time.Time, kind string, observedAt time.Time) *time.Time {
	if input != nil && !input.IsZero() {
		t := *input
		return &t
	}
	t := observedAt.Add(autoResolveWindow(kind))
	return &t
}

func autoResolveWindow(kind string) time.Duration {
	switch kind {
	case monitoring.KindAPIRequestError:
		return 30 * time.Minute
	case monitoring.KindSchedulerError:
		return 15 * time.Minute
	case monitoring.KindUpstreamAccountError:
		return time.Hour
	case monitoring.KindPluginError:
		return 30 * time.Minute
	case monitoring.KindTaskError:
		return 2 * time.Hour
	default:
		return time.Hour
	}
}

func defaultTitle(kind string) string {
	switch kind {
	case monitoring.KindAPIRequestError:
		return "API request error"
	case monitoring.KindSchedulerError:
		return "Scheduler error"
	case monitoring.KindUpstreamAccountError:
		return "Upstream account error"
	case monitoring.KindPluginError:
		return "Plugin error"
	case monitoring.KindTaskError:
		return "Task error"
	default:
		return "System monitor event"
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
