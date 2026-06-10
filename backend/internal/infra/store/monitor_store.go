package store

import (
	"context"
	"sort"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	entmonitorevent "github.com/DevilGenius/airgate-core/ent/monitorevent"
	"github.com/DevilGenius/airgate-core/ent/predicate"
	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

const (
	monitorSummaryTopLimit    = 5
	monitorSummaryRecentLimit = 10
)

// MonitorStore persists temporary monitor events with Ent.
type MonitorStore struct {
	db *ent.Client
}

// NewMonitorStore creates a monitor event store.
func NewMonitorStore(db *ent.Client) *MonitorStore {
	return &MonitorStore{db: db}
}

// InsertBatch appends monitor events. Hashes are classification keys only,
// not uniqueness keys.
func (s *MonitorStore) InsertBatch(ctx context.Context, events []appmonitor.QueuedEvent) error {
	if s == nil || s.db == nil || len(events) == 0 {
		return nil
	}
	builders := make([]*ent.MonitorEventCreate, 0, len(events))
	for _, event := range events {
		if event.Hash == "" {
			continue
		}
		builders = append(builders, setMonitorCreateFields(s.db.MonitorEvent.Create(), event))
	}
	if len(builders) == 0 {
		return nil
	}
	_, err := s.db.MonitorEvent.CreateBulk(builders...).Save(ctx)
	return err
}

// ResolveBySubject marks matching active events as resolved.
func (s *MonitorStore) ResolveBySubject(ctx context.Context, query monitoring.ResolveQuery) error {
	if s == nil || s.db == nil {
		return nil
	}
	preds := monitorResolvePredicates(query)
	if len(preds) == 0 {
		return nil
	}
	preds = append([]predicate.MonitorEvent{entmonitorevent.StatusEQ(entmonitorevent.StatusActive)}, preds...)
	_, err := s.db.MonitorEvent.Update().
		Where(preds...).
		SetStatus(entmonitorevent.StatusResolved).
		SetResolvedAt(time.Now()).
		ClearAutoResolveAt().
		Save(ctx)
	return err
}

// Get returns one monitor event by id.
func (s *MonitorStore) Get(ctx context.Context, id int) (appmonitor.Event, error) {
	if s == nil || s.db == nil || id <= 0 {
		return appmonitor.Event{}, appmonitor.ErrEventNotFound
	}
	row, err := s.db.MonitorEvent.Get(ctx, id)
	if ent.IsNotFound(err) {
		return appmonitor.Event{}, appmonitor.ErrEventNotFound
	}
	if err != nil {
		return appmonitor.Event{}, err
	}
	return mapMonitorEvent(row), nil
}

// Resolve marks one monitor event resolved.
func (s *MonitorStore) Resolve(ctx context.Context, id int) error {
	if s == nil || s.db == nil || id <= 0 {
		return appmonitor.ErrEventNotFound
	}
	now := time.Now()
	err := s.db.MonitorEvent.UpdateOneID(id).
		SetStatus(entmonitorevent.StatusResolved).
		SetResolvedAt(now).
		ClearIgnoredAt().
		ClearAutoResolveAt().
		ClearNextNotifyAt().
		SetNotifyError("").
		Exec(ctx)
	if ent.IsNotFound(err) {
		return appmonitor.ErrEventNotFound
	}
	return err
}

// Ignore marks one monitor event ignored.
func (s *MonitorStore) Ignore(ctx context.Context, id int) error {
	if s == nil || s.db == nil || id <= 0 {
		return appmonitor.ErrEventNotFound
	}
	now := time.Now()
	err := s.db.MonitorEvent.UpdateOneID(id).
		SetStatus(entmonitorevent.StatusIgnored).
		SetIgnoredAt(now).
		ClearResolvedAt().
		ClearAutoResolveAt().
		ClearNextNotifyAt().
		SetNotifyError("").
		Exec(ctx)
	if ent.IsNotFound(err) {
		return appmonitor.ErrEventNotFound
	}
	return err
}

// AutoResolveDue resolves active events whose quiet window has expired.
func (s *MonitorStore) AutoResolveDue(ctx context.Context, now time.Time, batchSize int) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	events, err := s.db.MonitorEvent.Query().
		Where(
			entmonitorevent.StatusEQ(entmonitorevent.StatusActive),
			entmonitorevent.AutoResolveAtNotNil(),
			entmonitorevent.AutoResolveAtLTE(now),
		).
		Order(ent.Asc(entmonitorevent.FieldAutoResolveAt), ent.Asc(entmonitorevent.FieldID)).
		Limit(batchSize).
		All(ctx)
	if err != nil {
		return 0, err
	}
	resolved := 0
	for _, event := range events {
		resolvedAt := now
		if event.AutoResolveAt != nil {
			resolvedAt = *event.AutoResolveAt
		}
		err := s.db.MonitorEvent.UpdateOneID(event.ID).
			Where(entmonitorevent.StatusEQ(entmonitorevent.StatusActive)).
			SetStatus(entmonitorevent.StatusResolved).
			SetResolvedAt(resolvedAt).
			Exec(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			return resolved, err
		}
		resolved++
	}
	return resolved, nil
}

// CleanupExpired deletes expired monitor events.
func (s *MonitorStore) CleanupExpired(ctx context.Context, cutoff time.Time, batchSize int) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	ids, err := s.db.MonitorEvent.Query().
		Where(entmonitorevent.ExpiresAtLT(cutoff)).
		Order(ent.Asc(entmonitorevent.FieldExpiresAt), ent.Asc(entmonitorevent.FieldID)).
		Limit(batchSize).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return s.db.MonitorEvent.Delete().
		Where(entmonitorevent.IDIn(ids...)).
		Exec(ctx)
}

// List returns a stable cursor page ordered by updated_at desc, id desc.
func (s *MonitorStore) List(ctx context.Context, filter appmonitor.ListFilter) (appmonitor.ListResult, error) {
	if s == nil || s.db == nil {
		return appmonitor.ListResult{}, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := s.db.MonitorEvent.Query()
	query = applyMonitorListFilter(query, filter)
	if filter.Cursor != nil && !filter.Cursor.UpdatedAt.IsZero() && filter.Cursor.ID > 0 {
		query = query.Where(entmonitorevent.Or(
			entmonitorevent.UpdatedAtLT(filter.Cursor.UpdatedAt),
			entmonitorevent.And(
				entmonitorevent.UpdatedAtEQ(filter.Cursor.UpdatedAt),
				entmonitorevent.IDLT(filter.Cursor.ID),
			),
		))
	}
	rows, err := query.
		Order(ent.Desc(entmonitorevent.FieldUpdatedAt), ent.Desc(entmonitorevent.FieldID)).
		Limit(limit + 1).
		All(ctx)
	if err != nil {
		return appmonitor.ListResult{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]appmonitor.Event, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapMonitorEvent(row))
	}
	var next *appmonitor.ListCursor
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = &appmonitor.ListCursor{
			UpdatedAt: last.UpdatedAt,
			ID:        last.ID,
		}
	}
	return appmonitor.ListResult{
		List:       items,
		HasMore:    hasMore,
		NextCursor: next,
	}, nil
}

// Summary returns active monitor event aggregates.
func (s *MonitorStore) Summary(ctx context.Context) (appmonitor.Summary, error) {
	if s == nil || s.db == nil {
		return appmonitor.Summary{}, nil
	}
	base := s.db.MonitorEvent.Query().
		Where(entmonitorevent.StatusEQ(entmonitorevent.StatusActive))

	activeTotal, err := base.Clone().Count(ctx)
	if err != nil {
		return appmonitor.Summary{}, err
	}
	criticalTotal, err := base.Clone().Where(entmonitorevent.SeverityEQ(entmonitorevent.SeverityCritical)).Count(ctx)
	if err != nil {
		return appmonitor.Summary{}, err
	}
	errorTotal, err := base.Clone().Where(entmonitorevent.SeverityEQ(entmonitorevent.SeverityError)).Count(ctx)
	if err != nil {
		return appmonitor.Summary{}, err
	}
	warningTotal, err := base.Clone().Where(entmonitorevent.SeverityEQ(entmonitorevent.SeverityWarning)).Count(ctx)
	if err != nil {
		return appmonitor.Summary{}, err
	}
	byType, err := s.summaryByType(ctx, base.Clone())
	if err != nil {
		return appmonitor.Summary{}, err
	}
	topAccounts, err := s.summaryTopAccounts(ctx, base.Clone())
	if err != nil {
		return appmonitor.Summary{}, err
	}
	recentRows, err := base.Clone().
		Order(ent.Desc(entmonitorevent.FieldUpdatedAt), ent.Desc(entmonitorevent.FieldID)).
		Limit(monitorSummaryRecentLimit).
		All(ctx)
	if err != nil {
		return appmonitor.Summary{}, err
	}
	recent := make([]appmonitor.Event, 0, len(recentRows))
	for _, row := range recentRows {
		recent = append(recent, mapMonitorEvent(row))
	}

	return appmonitor.Summary{
		ActiveTotal:   int64(activeTotal),
		CriticalTotal: int64(criticalTotal),
		ErrorTotal:    int64(errorTotal),
		WarningTotal:  int64(warningTotal),
		ByType:        byType,
		TopAccounts:   topAccounts,
		Recent:        recent,
	}, nil
}

func (s *MonitorStore) summaryByType(ctx context.Context, query *ent.MonitorEventQuery) ([]appmonitor.TypeCount, error) {
	var rows []struct {
		Type  string `json:"type"`
		Count int    `json:"count"`
	}
	err := query.GroupBy(entmonitorevent.FieldType).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]appmonitor.TypeCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, appmonitor.TypeCount{Type: row.Type, Count: int64(row.Count)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Type < out[j].Type
		}
		return out[i].Count > out[j].Count
	})
	return out, nil
}

func (s *MonitorStore) summaryTopAccounts(ctx context.Context, query *ent.MonitorEventQuery) ([]appmonitor.SubjectCount, error) {
	var rows []struct {
		AccountID           int    `json:"account_id"`
		AccountNameSnapshot string `json:"account_name_snapshot"`
		Count               int    `json:"count"`
	}
	err := query.
		Where(entmonitorevent.AccountIDNotNil()).
		GroupBy(entmonitorevent.FieldAccountID, entmonitorevent.FieldAccountNameSnapshot).
		Aggregate(ent.As(ent.Count(), "count")).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]appmonitor.SubjectCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, appmonitor.SubjectCount{
			ID:    row.AccountID,
			Name:  row.AccountNameSnapshot,
			Count: int64(row.Count),
		})
	}
	sortSubjectCounts(out)
	return limitSubjectCounts(out, monitorSummaryTopLimit), nil
}

func applyMonitorListFilter(query *ent.MonitorEventQuery, filter appmonitor.ListFilter) *ent.MonitorEventQuery {
	if filter.Status != "" {
		query = query.Where(entmonitorevent.StatusEQ(entmonitorevent.Status(filter.Status)))
	}
	if filter.Severity != "" {
		query = query.Where(entmonitorevent.SeverityEQ(entmonitorevent.Severity(filter.Severity)))
	}
	if filter.Type != "" {
		query = query.Where(entmonitorevent.TypeEQ(entmonitorevent.Type(filter.Type)))
	}
	if filter.Source != "" {
		query = query.Where(entmonitorevent.SourceEQ(filter.Source))
	}
	if filter.SubjectType != "" {
		query = query.Where(entmonitorevent.SubjectTypeEQ(filter.SubjectType))
	}
	if filter.AccountID != nil {
		query = query.Where(entmonitorevent.AccountIDEQ(*filter.AccountID))
	}
	if filter.Platform != "" {
		query = query.Where(entmonitorevent.PlatformEQ(filter.Platform))
	}
	if filter.PluginID != "" {
		query = query.Where(entmonitorevent.PluginIDEQ(filter.PluginID))
	}
	if filter.TaskType != "" {
		query = query.Where(entmonitorevent.TaskTypeEQ(filter.TaskType))
	}
	if filter.ErrorCode != "" {
		query = query.Where(entmonitorevent.ErrorCodeEQ(filter.ErrorCode))
	}
	if filter.From != nil {
		query = query.Where(entmonitorevent.UpdatedAtGTE(*filter.From))
	}
	if filter.To != nil {
		query = query.Where(entmonitorevent.UpdatedAtLTE(*filter.To))
	}
	return query
}

func monitorResolvePredicates(query monitoring.ResolveQuery) []predicate.MonitorEvent {
	preds := make([]predicate.MonitorEvent, 0, 8)
	if query.Type != "" {
		preds = append(preds, entmonitorevent.TypeEQ(entmonitorevent.Type(query.Type)))
	}
	if query.SubjectType != "" {
		preds = append(preds, entmonitorevent.SubjectTypeEQ(query.SubjectType))
	}
	if query.SubjectID != "" {
		preds = append(preds, entmonitorevent.SubjectIDEQ(query.SubjectID))
	}
	if query.AccountID != nil {
		preds = append(preds, entmonitorevent.AccountIDEQ(*query.AccountID))
	}
	if query.PluginID != "" {
		preds = append(preds, entmonitorevent.PluginIDEQ(query.PluginID))
	}
	if query.TaskType != "" {
		preds = append(preds, entmonitorevent.TaskTypeEQ(query.TaskType))
	}
	if query.ErrorCode != "" {
		preds = append(preds, entmonitorevent.ErrorCodeEQ(query.ErrorCode))
	}
	return preds
}

func setMonitorCreateFields(create *ent.MonitorEventCreate, event appmonitor.QueuedEvent) *ent.MonitorEventCreate {
	create = create.
		SetType(entmonitorevent.Type(event.Type)).
		SetSeverity(entmonitorevent.Severity(event.Severity)).
		SetStatus(entmonitorevent.StatusActive).
		SetSource(event.Source).
		SetSubjectType(event.SubjectType).
		SetSubjectID(event.SubjectID).
		SetHash(event.Hash).
		SetTitle(event.Title).
		SetMessage(event.Message).
		SetNillableAccountID(event.AccountID).
		SetAccountNameSnapshot(event.AccountNameSnapshot).
		SetPlatform(event.Platform).
		SetPluginID(event.PluginID).
		SetTaskType(event.TaskType).
		SetErrorCode(event.ErrorCode).
		SetCreatedAt(event.CreatedAt).
		SetUpdatedAt(event.UpdatedAt).
		SetNillableAutoResolveAt(event.AutoResolveAt).
		SetExpiresAt(event.ExpiresAt).
		SetDetail(event.Detail)
	if shouldNotifySeverity(event.Severity) {
		create.SetNextNotifyAt(event.UpdatedAt)
	}
	return create
}

func mapMonitorEvent(row *ent.MonitorEvent) appmonitor.Event {
	if row == nil {
		return appmonitor.Event{}
	}
	detail := row.Detail
	if detail == nil {
		detail = map[string]interface{}{}
	}
	return appmonitor.Event{
		ID:                  row.ID,
		Type:                string(row.Type),
		Severity:            string(row.Severity),
		Status:              string(row.Status),
		Source:              row.Source,
		SubjectType:         row.SubjectType,
		SubjectID:           row.SubjectID,
		Hash:                row.Hash,
		Title:               row.Title,
		Message:             row.Message,
		AccountID:           row.AccountID,
		AccountNameSnapshot: row.AccountNameSnapshot,
		Platform:            row.Platform,
		PluginID:            row.PluginID,
		TaskType:            row.TaskType,
		ErrorCode:           row.ErrorCode,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ResolvedAt:          row.ResolvedAt,
		IgnoredAt:           row.IgnoredAt,
		AutoResolveAt:       row.AutoResolveAt,
		ExpiresAt:           row.ExpiresAt,
		LastNotifiedAt:      row.LastNotifiedAt,
		NextNotifyAt:        row.NextNotifyAt,
		NotifyError:         row.NotifyError,
		Detail:              detail,
	}
}

// ListNotifyDue returns active error/critical events ready to notify.
func (s *MonitorStore) ListNotifyDue(ctx context.Context, now time.Time, batchSize int) ([]appmonitor.Event, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	rows, err := s.db.MonitorEvent.Query().
		Where(
			entmonitorevent.StatusEQ(entmonitorevent.StatusActive),
			entmonitorevent.SeverityIn(entmonitorevent.SeverityError, entmonitorevent.SeverityCritical),
			entmonitorevent.NextNotifyAtNotNil(),
			entmonitorevent.NextNotifyAtLTE(now),
		).
		Order(ent.Asc(entmonitorevent.FieldNextNotifyAt), ent.Asc(entmonitorevent.FieldID)).
		Limit(batchSize).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]appmonitor.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapMonitorEvent(row))
	}
	return out, nil
}

// MarkNotified stores a successful notification attempt.
func (s *MonitorStore) MarkNotified(ctx context.Context, id int, notifiedAt time.Time, nextNotifyAt time.Time) error {
	if s == nil || s.db == nil || id <= 0 {
		return nil
	}
	err := s.db.MonitorEvent.UpdateOneID(id).
		Where(entmonitorevent.StatusEQ(entmonitorevent.StatusActive)).
		SetLastNotifiedAt(notifiedAt).
		SetNextNotifyAt(nextNotifyAt).
		SetNotifyError("").
		Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

// MarkNotifyFailed stores a failed notification attempt and retry time.
func (s *MonitorStore) MarkNotifyFailed(ctx context.Context, id int, retryAt time.Time, reason string) error {
	if s == nil || s.db == nil || id <= 0 {
		return nil
	}
	err := s.db.MonitorEvent.UpdateOneID(id).
		Where(entmonitorevent.StatusEQ(entmonitorevent.StatusActive)).
		SetNextNotifyAt(retryAt).
		SetNotifyError(truncateStoreString(reason, 500)).
		Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}

func shouldNotifySeverity(severity string) bool {
	return severity == monitoring.SeverityError || severity == monitoring.SeverityCritical
}

func truncateStoreString(value string, limit int) string {
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func sortSubjectCounts(items []appmonitor.SubjectCount) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].ID < items[j].ID
		}
		return items[i].Count > items[j].Count
	})
}

func limitSubjectCounts(items []appmonitor.SubjectCount, limit int) []appmonitor.SubjectCount {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}
