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

// UpsertBatch applies already aggregated monitor events.
func (s *MonitorStore) UpsertBatch(ctx context.Context, events []appmonitor.AggregatedEvent) error {
	if s == nil || s.db == nil || len(events) == 0 {
		return nil
	}
	for _, event := range events {
		if event.Fingerprint == "" {
			continue
		}
		if event.CountDelta <= 0 {
			event.CountDelta = 1
		}
		if err := s.upsertOne(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (s *MonitorStore) upsertOne(ctx context.Context, incoming appmonitor.AggregatedEvent) error {
	existing, err := s.db.MonitorEvent.Query().
		Where(entmonitorevent.FingerprintEQ(incoming.Fingerprint)).
		Only(ctx)
	if ent.IsNotFound(err) {
		_, err = setMonitorCreateFields(s.db.MonitorEvent.Create(), incoming).Save(ctx)
		if err == nil {
			return nil
		}
		if !ent.IsConstraintError(err) {
			return err
		}
		existing, err = s.db.MonitorEvent.Query().
			Where(entmonitorevent.FingerprintEQ(incoming.Fingerprint)).
			Only(ctx)
	}
	if err != nil {
		return err
	}

	switch string(existing.Status) {
	case monitoring.StatusActive:
		return s.mergeActive(ctx, existing, incoming)
	case monitoring.StatusResolved:
		return s.mergeResolved(ctx, existing, incoming)
	case monitoring.StatusIgnored:
		return s.mergeIgnored(ctx, existing, incoming)
	default:
		return s.mergeActive(ctx, existing, incoming)
	}
}

func (s *MonitorStore) mergeActive(ctx context.Context, existing *ent.MonitorEvent, incoming appmonitor.AggregatedEvent) error {
	update := s.db.MonitorEvent.UpdateOneID(existing.ID).
		AddCount(incoming.CountDelta).
		SetSeverity(entmonitorevent.Severity(higherMonitorSeverity(string(existing.Severity), incoming.Severity)))
	if !incoming.UpdatedAt.Before(existing.UpdatedAt) {
		update = setMonitorUpdateFields(update, incoming, false).
			SetUpdatedAt(incoming.UpdatedAt).
			SetExpiresAt(incoming.ExpiresAt)
		update.SetNillableAutoResolveAt(incoming.AutoResolveAt)
	}
	if err := update.Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	return nil
}

func (s *MonitorStore) mergeResolved(ctx context.Context, existing *ent.MonitorEvent, incoming appmonitor.AggregatedEvent) error {
	if existing.ResolvedAt != nil && !incoming.UpdatedAt.After(*existing.ResolvedAt) {
		return nil
	}
	update := s.db.MonitorEvent.UpdateOneID(existing.ID).
		SetStatus(entmonitorevent.StatusActive).
		SetSeverity(entmonitorevent.Severity(incoming.Severity)).
		SetCount(incoming.CountDelta).
		SetCreatedAt(incoming.CreatedAt).
		SetUpdatedAt(incoming.UpdatedAt).
		SetExpiresAt(incoming.ExpiresAt).
		ClearResolvedAt().
		ClearIgnoredAt()
	update = setMonitorUpdateFields(update, incoming, true)
	update.SetNillableAutoResolveAt(incoming.AutoResolveAt)
	if err := update.Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	return nil
}

func (s *MonitorStore) mergeIgnored(ctx context.Context, existing *ent.MonitorEvent, incoming appmonitor.AggregatedEvent) error {
	update := s.db.MonitorEvent.UpdateOneID(existing.ID).
		AddCount(incoming.CountDelta).
		SetSeverity(entmonitorevent.Severity(higherMonitorSeverity(string(existing.Severity), incoming.Severity)))
	if !incoming.UpdatedAt.Before(existing.UpdatedAt) {
		update = setMonitorUpdateFields(update, incoming, false).
			SetUpdatedAt(incoming.UpdatedAt)
	}
	if err := update.Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return err
	}
	return nil
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
	byKind, err := s.summaryByKind(ctx, base.Clone())
	if err != nil {
		return appmonitor.Summary{}, err
	}
	topAPIKeys, err := s.summaryTopAPIKeys(ctx, base.Clone())
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
		ByKind:        byKind,
		TopAPIKeys:    topAPIKeys,
		TopAccounts:   topAccounts,
		Recent:        recent,
	}, nil
}

func (s *MonitorStore) summaryByKind(ctx context.Context, query *ent.MonitorEventQuery) ([]appmonitor.KindCount, error) {
	var rows []struct {
		Kind  string `json:"kind"`
		Count int    `json:"count"`
	}
	err := query.GroupBy(entmonitorevent.FieldKind).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]appmonitor.KindCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, appmonitor.KindCount{Kind: row.Kind, Count: int64(row.Count)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Count > out[j].Count
	})
	return out, nil
}

func (s *MonitorStore) summaryTopAPIKeys(ctx context.Context, query *ent.MonitorEventQuery) ([]appmonitor.SubjectCount, error) {
	var rows []struct {
		APIKeyID           int    `json:"api_key_id"`
		APIKeyNameSnapshot string `json:"api_key_name_snapshot"`
		Count              int64  `json:"count"`
	}
	err := query.
		Where(entmonitorevent.APIKeyIDNotNil()).
		GroupBy(entmonitorevent.FieldAPIKeyID, entmonitorevent.FieldAPIKeyNameSnapshot).
		Aggregate(ent.As(ent.Sum(entmonitorevent.FieldCount), "count")).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]appmonitor.SubjectCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, appmonitor.SubjectCount{
			ID:    row.APIKeyID,
			Name:  row.APIKeyNameSnapshot,
			Count: row.Count,
		})
	}
	sortSubjectCounts(out)
	return limitSubjectCounts(out, monitorSummaryTopLimit), nil
}

func (s *MonitorStore) summaryTopAccounts(ctx context.Context, query *ent.MonitorEventQuery) ([]appmonitor.SubjectCount, error) {
	var rows []struct {
		AccountID           int    `json:"account_id"`
		AccountNameSnapshot string `json:"account_name_snapshot"`
		Count               int64  `json:"count"`
	}
	err := query.
		Where(entmonitorevent.AccountIDNotNil()).
		GroupBy(entmonitorevent.FieldAccountID, entmonitorevent.FieldAccountNameSnapshot).
		Aggregate(ent.As(ent.Sum(entmonitorevent.FieldCount), "count")).
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]appmonitor.SubjectCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, appmonitor.SubjectCount{
			ID:    row.AccountID,
			Name:  row.AccountNameSnapshot,
			Count: row.Count,
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
	if filter.Kind != "" {
		query = query.Where(entmonitorevent.KindEQ(entmonitorevent.Kind(filter.Kind)))
	}
	if filter.Source != "" {
		query = query.Where(entmonitorevent.SourceEQ(filter.Source))
	}
	if filter.SubjectType != "" {
		query = query.Where(entmonitorevent.SubjectTypeEQ(filter.SubjectType))
	}
	if filter.APIKeyID != nil {
		query = query.Where(entmonitorevent.APIKeyIDEQ(*filter.APIKeyID))
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
	if filter.Endpoint != "" {
		query = query.Where(entmonitorevent.EndpointEQ(filter.Endpoint))
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
	if query.Kind != "" {
		preds = append(preds, entmonitorevent.KindEQ(entmonitorevent.Kind(query.Kind)))
	}
	if query.SubjectType != "" {
		preds = append(preds, entmonitorevent.SubjectTypeEQ(query.SubjectType))
	}
	if query.SubjectID != "" {
		preds = append(preds, entmonitorevent.SubjectIDEQ(query.SubjectID))
	}
	if query.APIKeyID != nil {
		preds = append(preds, entmonitorevent.APIKeyIDEQ(*query.APIKeyID))
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

func setMonitorCreateFields(create *ent.MonitorEventCreate, event appmonitor.AggregatedEvent) *ent.MonitorEventCreate {
	return create.
		SetKind(entmonitorevent.Kind(event.Kind)).
		SetSeverity(entmonitorevent.Severity(event.Severity)).
		SetStatus(entmonitorevent.StatusActive).
		SetSource(event.Source).
		SetSubjectType(event.SubjectType).
		SetSubjectID(event.SubjectID).
		SetFingerprint(event.Fingerprint).
		SetTitle(event.Title).
		SetMessage(event.Message).
		SetNillableAPIKeyID(event.APIKeyID).
		SetAPIKeyNameSnapshot(event.APIKeyNameSnapshot).
		SetAPIKeyPrefix(event.APIKeyPrefix).
		SetNillableUserID(event.UserID).
		SetUserEmailSnapshot(event.UserEmailSnapshot).
		SetNillableGroupID(event.GroupID).
		SetNillableAccountID(event.AccountID).
		SetAccountNameSnapshot(event.AccountNameSnapshot).
		SetPlatform(event.Platform).
		SetPluginID(event.PluginID).
		SetTaskType(event.TaskType).
		SetMethod(event.Method).
		SetEndpoint(event.Endpoint).
		SetRequestPath(event.RequestPath).
		SetModel(event.Model).
		SetNillableHTTPStatus(event.HTTPStatus).
		SetNillableUpstreamStatus(event.UpstreamStatus).
		SetErrorCode(event.ErrorCode).
		SetErrorType(event.ErrorType).
		SetCount(event.CountDelta).
		SetCreatedAt(event.CreatedAt).
		SetUpdatedAt(event.UpdatedAt).
		SetNillableAutoResolveAt(event.AutoResolveAt).
		SetExpiresAt(event.ExpiresAt).
		SetDetail(event.Detail)
}

func setMonitorUpdateFields(update *ent.MonitorEventUpdateOne, event appmonitor.AggregatedEvent, replace bool) *ent.MonitorEventUpdateOne {
	update = update.
		SetKind(entmonitorevent.Kind(event.Kind)).
		SetSource(event.Source).
		SetSubjectType(event.SubjectType).
		SetSubjectID(event.SubjectID).
		SetTitle(event.Title).
		SetMessage(event.Message).
		SetAPIKeyNameSnapshot(event.APIKeyNameSnapshot).
		SetAPIKeyPrefix(event.APIKeyPrefix).
		SetUserEmailSnapshot(event.UserEmailSnapshot).
		SetAccountNameSnapshot(event.AccountNameSnapshot).
		SetPlatform(event.Platform).
		SetPluginID(event.PluginID).
		SetTaskType(event.TaskType).
		SetMethod(event.Method).
		SetEndpoint(event.Endpoint).
		SetRequestPath(event.RequestPath).
		SetModel(event.Model).
		SetErrorCode(event.ErrorCode).
		SetErrorType(event.ErrorType).
		SetDetail(event.Detail)
	if event.APIKeyID != nil {
		update.SetAPIKeyID(*event.APIKeyID)
	} else if replace {
		update.ClearAPIKeyID()
	}
	if event.UserID != nil {
		update.SetUserID(*event.UserID)
	} else if replace {
		update.ClearUserID()
	}
	if event.GroupID != nil {
		update.SetGroupID(*event.GroupID)
	} else if replace {
		update.ClearGroupID()
	}
	if event.AccountID != nil {
		update.SetAccountID(*event.AccountID)
	} else if replace {
		update.ClearAccountID()
	}
	if event.HTTPStatus != nil {
		update.SetHTTPStatus(*event.HTTPStatus)
	} else if replace {
		update.ClearHTTPStatus()
	}
	if event.UpstreamStatus != nil {
		update.SetUpstreamStatus(*event.UpstreamStatus)
	} else if replace {
		update.ClearUpstreamStatus()
	}
	return update
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
		Kind:                string(row.Kind),
		Severity:            string(row.Severity),
		Status:              string(row.Status),
		Source:              row.Source,
		SubjectType:         row.SubjectType,
		SubjectID:           row.SubjectID,
		Fingerprint:         row.Fingerprint,
		Title:               row.Title,
		Message:             row.Message,
		APIKeyID:            row.APIKeyID,
		APIKeyNameSnapshot:  row.APIKeyNameSnapshot,
		APIKeyPrefix:        row.APIKeyPrefix,
		UserID:              row.UserID,
		UserEmailSnapshot:   row.UserEmailSnapshot,
		GroupID:             row.GroupID,
		AccountID:           row.AccountID,
		AccountNameSnapshot: row.AccountNameSnapshot,
		Platform:            row.Platform,
		PluginID:            row.PluginID,
		TaskType:            row.TaskType,
		Method:              row.Method,
		Endpoint:            row.Endpoint,
		RequestPath:         row.RequestPath,
		Model:               row.Model,
		HTTPStatus:          row.HTTPStatus,
		UpstreamStatus:      row.UpstreamStatus,
		ErrorCode:           row.ErrorCode,
		ErrorType:           row.ErrorType,
		Count:               row.Count,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ResolvedAt:          row.ResolvedAt,
		IgnoredAt:           row.IgnoredAt,
		AutoResolveAt:       row.AutoResolveAt,
		ExpiresAt:           row.ExpiresAt,
		Detail:              detail,
	}
}

func higherMonitorSeverity(left, right string) string {
	if monitorSeverityRank(right) > monitorSeverityRank(left) {
		return right
	}
	return left
}

func monitorSeverityRank(severity string) int {
	switch severity {
	case monitoring.SeverityCritical:
		return 3
	case monitoring.SeverityError:
		return 2
	case monitoring.SeverityWarning:
		return 1
	default:
		return 0
	}
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
