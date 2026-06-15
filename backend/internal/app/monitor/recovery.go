package monitor

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

const monitorRecoveryZSetKey = "monitor:recovery:keys"
const successRecoveryFallbackWindow = time.Hour

var recoverySchedulerErrorCodes = []string{
	"no_available_account",
	"all_routes_account_unavailable",
}

type recoverySnapshot struct {
	mu    sync.Mutex
	state atomic.Value
}

type recoverySnapshotState struct {
	entries map[string]time.Time
	active  bool
}

func newRecoverySnapshot() *recoverySnapshot {
	r := &recoverySnapshot{}
	r.store(map[string]time.Time{}, time.Now())
	return r
}

func (r *recoverySnapshot) remember(hash string, expiresAt time.Time) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(defaultRetention)
	}
	r.mu.Lock()
	entries := cloneRecoveryEntries(r.snapshot().entries)
	entries[hash] = expiresAt
	r.store(entries, time.Now())
	r.mu.Unlock()
}

func (r *recoverySnapshot) forget(hash string) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return
	}
	r.mu.Lock()
	entries := cloneRecoveryEntries(r.snapshot().entries)
	delete(entries, hash)
	r.store(entries, time.Now())
	r.mu.Unlock()
}

func (r *recoverySnapshot) has(hash string, now time.Time) bool {
	hash = strings.TrimSpace(hash)
	state := r.snapshot()
	if hash == "" || !state.active {
		return false
	}
	expiresAt, ok := state.entries[hash]
	if !ok {
		return false
	}
	if !expiresAt.IsZero() && !expiresAt.After(now) {
		return false
	}
	return true
}

func (r *recoverySnapshot) hasActive() bool {
	return r.snapshot().active
}

func (r *recoverySnapshot) merge(entries map[string]time.Time, now time.Time) {
	r.mu.Lock()
	current := r.snapshot().entries
	merged := make(map[string]time.Time, len(current)+len(entries))
	for hash, expiresAt := range current {
		if !expiresAt.IsZero() && !expiresAt.After(now) {
			continue
		}
		merged[hash] = expiresAt
	}
	for hash, expiresAt := range entries {
		merged[hash] = expiresAt
	}
	r.store(merged, now)
	r.mu.Unlock()
}

func (r *recoverySnapshot) pruneExpired(now time.Time) {
	r.mu.Lock()
	current := r.snapshot().entries
	pruned := make(map[string]time.Time, len(current))
	for hash, expiresAt := range current {
		if !expiresAt.IsZero() && !expiresAt.After(now) {
			continue
		}
		pruned[hash] = expiresAt
	}
	r.store(pruned, now)
	r.mu.Unlock()
}

func (r *recoverySnapshot) snapshot() recoverySnapshotState {
	state, ok := r.state.Load().(recoverySnapshotState)
	if !ok || state.entries == nil {
		return recoverySnapshotState{entries: map[string]time.Time{}}
	}
	return state
}

func (r *recoverySnapshot) store(entries map[string]time.Time, now time.Time) {
	r.state.Store(recoverySnapshotState{
		entries: entries,
		active:  recoveryEntriesHaveActive(entries, now),
	})
}

func cloneRecoveryEntries(entries map[string]time.Time) map[string]time.Time {
	if len(entries) == 0 {
		return make(map[string]time.Time)
	}
	clone := make(map[string]time.Time, len(entries))
	for hash, expiresAt := range entries {
		clone[hash] = expiresAt
	}
	return clone
}

func recoveryEntriesHaveActive(entries map[string]time.Time, now time.Time) bool {
	for _, expiresAt := range entries {
		if expiresAt.IsZero() || expiresAt.After(now) {
			return true
		}
	}
	return false
}

func (s *Service) RecordRecoverySuccess(ctx context.Context, input monitoring.RecoverySuccess) {
	if s == nil || s.recovery == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	now := time.Now()
	if !s.recovery.hasActive() {
		return
	}
	for _, query := range recoveryResolveQueries(input) {
		if !s.recovery.has(query.Hash, now) {
			continue
		}
		select {
		case s.queue <- queuedOperation{Kind: queuedOperationResolve, Resolve: cloneResolveQuery(query)}:
			s.queuedEvents.Add(1)
		default:
			dropped := s.droppedEvents.Add(1)
			s.logDrop(dropped)
		}
	}
}

func (s *Service) rememberRecoveryEvent(event QueuedEvent) {
	if s == nil || s.recovery == nil {
		return
	}
	if event.RecoveryMode != monitoring.RecoveryModeSuccess {
		return
	}
	hash, ok := recoveryHashForEvent(event.Event)
	if !ok {
		return
	}
	s.recovery.remember(hash, recoveryKeyExpiresAt(event.Event))
}

func (s *Service) persistRecoveryEvents(ctx context.Context, events []QueuedEvent) {
	if s == nil || s.rdb == nil || len(events) == 0 {
		return
	}
	items := make([]redis.Z, 0, len(events))
	for _, event := range events {
		if event.RecoveryMode != monitoring.RecoveryModeSuccess {
			continue
		}
		hash, ok := recoveryHashForEvent(event.Event)
		if !ok {
			continue
		}
		expiresAt := recoveryKeyExpiresAt(event.Event)
		items = append(items, redis.Z{
			Score:  float64(expiresAt.Unix()),
			Member: hash,
		})
	}
	if len(items) == 0 {
		return
	}
	if err := s.rdb.ZAdd(ctx, monitorRecoveryZSetKey, items...).Err(); err != nil {
		slog.Debug("monitor_recovery_key_persist_failed", "error", err)
	}
}

func (s *Service) loadRecoverySnapshot(ctx context.Context) {
	if s == nil || s.rdb == nil || s.recovery == nil {
		return
	}
	now := time.Now()
	_, _ = s.rdb.ZRemRangeByScore(ctx, monitorRecoveryZSetKey, "-inf", strconv.FormatInt(now.Unix(), 10)).Result()
	rows, err := s.rdb.ZRangeByScoreWithScores(ctx, monitorRecoveryZSetKey, &redis.ZRangeBy{
		Min: strconv.FormatInt(now.Unix(), 10),
		Max: "+inf",
	}).Result()
	if err != nil {
		slog.Debug("monitor_recovery_snapshot_load_failed", "error", err)
		return
	}
	entries := make(map[string]time.Time, len(rows))
	for _, row := range rows {
		hash, ok := row.Member.(string)
		if !ok || strings.TrimSpace(hash) == "" {
			continue
		}
		entries[hash] = time.Unix(int64(row.Score), 0)
	}
	s.recovery.merge(entries, now)
}

func (s *Service) pruneRecoverySnapshot(ctx context.Context) {
	if s == nil {
		return
	}
	now := time.Now()
	if s.recovery != nil {
		s.recovery.pruneExpired(now)
	}
	if s.rdb != nil {
		_, _ = s.rdb.ZRemRangeByScore(ctx, monitorRecoveryZSetKey, "-inf", strconv.FormatInt(now.Unix(), 10)).Result()
	}
}

func (s *Service) forgetRecoveryQuery(ctx context.Context, query monitoring.ResolveQuery) {
	if s == nil || s.recovery == nil || strings.TrimSpace(query.Hash) == "" {
		return
	}
	s.recovery.forget(query.Hash)
	if s.rdb != nil {
		_, _ = s.rdb.ZRem(ctx, monitorRecoveryZSetKey, query.Hash).Result()
	}
}

func (s *Service) forgetRecoveryEvent(ctx context.Context, event Event) {
	hash, ok := recoveryHashForEvent(event)
	if !ok {
		return
	}
	s.forgetRecoveryQuery(ctx, monitoring.ResolveQuery{Hash: hash})
}

func recoveryHashForEvent(event Event) (string, bool) {
	if event.Type != monitoring.TypeSchedulerError || event.SubjectType != monitoring.SubjectScheduler {
		return "", false
	}
	if !isRecoverySchedulerErrorCode(event.ErrorCode) {
		return "", false
	}
	if monitorDetailString(event.Detail, "model") == "" {
		return "", false
	}
	if strings.TrimSpace(event.Hash) != "" {
		return event.Hash, true
	}
	return hashFor("", event), true
}

func nonManualRecoverableEvent(event Event) bool {
	mode := event.RecoveryMode
	if mode == "" {
		mode = recoveryModeForEvent(event)
	}
	return mode == monitoring.RecoveryModeNone || mode == monitoring.RecoveryModeExternal
}

func recoveryModeForEvent(event Event) string {
	if !recoverableSeverity(event.Severity) {
		return monitoring.RecoveryModeNone
	}
	if event.RecoveryMode != "" {
		return event.RecoveryMode
	}
	if externalRecoveryEvent(event) {
		return monitoring.RecoveryModeExternal
	}
	if _, ok := recoveryHashForEvent(event); ok {
		return monitoring.RecoveryModeSuccess
	}
	return monitoring.RecoveryModeManual
}

func recoverableSeverity(severity string) bool {
	return severity == monitoring.SeverityError || severity == monitoring.SeverityCritical
}

func externalRecoveryEvent(event Event) bool {
	return event.Type == monitoring.TypeUpstreamAccountError && (event.ErrorCode == "account_dead" || event.ErrorCode == "account_disabled" || event.ErrorCode == "reauth_required")
}

func recoveryFallbackAt(input *time.Time, observedAt time.Time) *time.Time {
	if input != nil && !input.IsZero() {
		t := *input
		return &t
	}
	t := observedAt.Add(successRecoveryFallbackWindow)
	return &t
}

func recoveryKeyExpiresAt(event Event) time.Time {
	if event.AutoResolveAt != nil && !event.AutoResolveAt.IsZero() {
		return *event.AutoResolveAt
	}
	if !event.ExpiresAt.IsZero() {
		return event.ExpiresAt
	}
	return time.Now().Add(successRecoveryFallbackWindow)
}

func recoveryResolveQueries(input monitoring.RecoverySuccess) []monitoring.ResolveQuery {
	event := recoveryEventFromSuccess(input)
	if event.Type == "" || event.SubjectType == "" || event.SubjectID == "" || monitorDetailString(event.Detail, "model") == "" {
		return nil
	}
	out := make([]monitoring.ResolveQuery, 0, len(recoverySchedulerErrorCodes))
	for _, code := range recoverySchedulerErrorCodes {
		event.ErrorCode = code
		hash := hashFor("", event)
		out = append(out, monitoring.ResolveQuery{
			Hash:        hash,
			Type:        event.Type,
			SubjectType: event.SubjectType,
			SubjectID:   event.SubjectID,
			ErrorCode:   code,
		})
	}
	return out
}

func recoveryEventFromSuccess(input monitoring.RecoverySuccess) Event {
	eventType := strings.TrimSpace(input.Type)
	if eventType == "" {
		eventType = monitoring.TypeSchedulerError
	}
	subjectType := strings.TrimSpace(input.SubjectType)
	if subjectType == "" {
		subjectType = monitoring.SubjectScheduler
	}
	platform := truncateString(input.Platform, maxPlatformLength)
	model := truncateString(input.Model, maxPlatformLength)
	subjectID := strings.TrimSpace(input.SubjectID)
	if subjectID == "" && input.GroupID > 0 {
		subjectID = strconv.Itoa(input.GroupID)
	}
	if subjectID == "" {
		subjectID = platform
	}
	return Event{
		Type:        eventType,
		SubjectType: subjectType,
		SubjectID:   truncateString(subjectID, maxSubjectIDLength),
		Platform:    platform,
		PluginID:    truncateString(input.PluginID, maxPlatformLength),
		Detail: map[string]interface{}{
			"model": model,
		},
	}
}

func isRecoverySchedulerErrorCode(code string) bool {
	for _, item := range recoverySchedulerErrorCodes {
		if code == item {
			return true
		}
	}
	return false
}

func monitorDetailString(detail map[string]interface{}, key string) string {
	value := detail[key]
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}
