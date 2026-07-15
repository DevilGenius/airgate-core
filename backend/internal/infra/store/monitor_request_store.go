package store

import (
	"context"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	entmonitorrequestevent "github.com/DevilGenius/airgate-core/ent/monitorrequestevent"
	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
)

// InsertRequestBatch appends request monitor events. Hashes are
// classification keys only, not uniqueness keys.
func (s *MonitorStore) InsertRequestBatch(ctx context.Context, events []appmonitor.QueuedRequestEvent) error {
	if s == nil || s.db == nil || len(events) == 0 {
		return nil
	}
	builders := make([]*ent.MonitorRequestEventCreate, 0, len(events))
	for _, event := range events {
		if event.Hash == "" {
			continue
		}
		builders = append(builders, setMonitorRequestCreateFields(s.db.MonitorRequestEvent.Create(), event))
	}
	if len(builders) == 0 {
		return nil
	}
	_, err := s.db.MonitorRequestEvent.CreateBulk(builders...).Save(ctx)
	return err
}

// ListRequests returns a stable cursor page ordered by created_at desc, id desc.
func (s *MonitorStore) ListRequests(ctx context.Context, filter appmonitor.RequestListFilter) (appmonitor.RequestListResult, error) {
	if s == nil || s.db == nil {
		return appmonitor.RequestListResult{}, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := s.db.MonitorRequestEvent.Query()
	query = applyMonitorRequestListFilter(query, filter)
	if filter.Cursor != nil && !filter.Cursor.CreatedAt.IsZero() && filter.Cursor.ID > 0 {
		query = query.Where(entmonitorrequestevent.Or(
			entmonitorrequestevent.CreatedAtLT(filter.Cursor.CreatedAt),
			entmonitorrequestevent.And(
				entmonitorrequestevent.CreatedAtEQ(filter.Cursor.CreatedAt),
				entmonitorrequestevent.IDLT(filter.Cursor.ID),
			),
		))
	}
	rows, err := query.
		Order(ent.Desc(entmonitorrequestevent.FieldCreatedAt), ent.Desc(entmonitorrequestevent.FieldID)).
		Limit(limit + 1).
		All(ctx)
	if err != nil {
		return appmonitor.RequestListResult{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]appmonitor.RequestEvent, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapMonitorRequestEvent(row))
	}
	var next *appmonitor.RequestListCursor
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		next = &appmonitor.RequestListCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		}
	}
	return appmonitor.RequestListResult{
		List:       items,
		HasMore:    hasMore,
		NextCursor: next,
	}, nil
}

// RequestSummary returns request monitor event aggregates. Request events only
// have warning/info severities and do not carry active/resolved state.
func (s *MonitorStore) RequestSummary(ctx context.Context) (appmonitor.Summary, error) {
	if s == nil || s.db == nil {
		return appmonitor.Summary{}, nil
	}
	base := s.db.MonitorRequestEvent.Query()
	severityCounts, err := s.requestSummarySeverityCounts(ctx, base.Clone())
	if err != nil {
		return appmonitor.Summary{}, err
	}
	now := time.Now()
	shortSeverityCounts, err := s.requestSummarySeverityCounts(ctx, base.Clone().
		Where(entmonitorrequestevent.CreatedAtGTE(now.Add(-monitorSummaryShortWindow))))
	if err != nil {
		return appmonitor.Summary{}, err
	}
	longSeverityCounts, err := s.requestSummarySeverityCounts(ctx, base.Clone().
		Where(entmonitorrequestevent.CreatedAtGTE(now.Add(-monitorSummaryLongWindow))))
	if err != nil {
		return appmonitor.Summary{}, err
	}
	severityCounts.Warning5MTotal = shortSeverityCounts.WarningTotal
	severityCounts.Warning1HTotal = longSeverityCounts.WarningTotal
	severityCounts.Info5MTotal = shortSeverityCounts.InfoTotal
	severityCounts.Info1HTotal = longSeverityCounts.InfoTotal
	return severityCounts, nil
}

func (s *MonitorStore) requestSummarySeverityCounts(ctx context.Context, query *ent.MonitorRequestEventQuery) (appmonitor.Summary, error) {
	var rows []struct {
		Severity string `json:"severity"`
		Count    int    `json:"count"`
	}
	err := query.
		GroupBy(entmonitorrequestevent.FieldSeverity).
		Aggregate(ent.Count()).
		Scan(ctx, &rows)
	if err != nil {
		return appmonitor.Summary{}, err
	}

	var out appmonitor.Summary
	for _, row := range rows {
		count := int64(row.Count)
		switch row.Severity {
		case string(entmonitorrequestevent.SeverityWarning):
			out.WarningTotal += count
		case string(entmonitorrequestevent.SeverityInfo):
			out.InfoTotal += count
		}
	}
	return out, nil
}

// ClearRequestEvents deletes request monitor rows. A nil before value clears all rows.
func (s *MonitorStore) ClearRequestEvents(ctx context.Context, before *time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	delete := s.db.MonitorRequestEvent.Delete()
	if before != nil && !before.IsZero() {
		delete = delete.Where(entmonitorrequestevent.CreatedAtLT(*before))
	}
	return delete.Exec(ctx)
}

// CleanupExpiredRequests deletes expired request monitor events.
func (s *MonitorStore) CleanupExpiredRequests(ctx context.Context, cutoff time.Time, batchSize int) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	ids, err := s.db.MonitorRequestEvent.Query().
		Where(entmonitorrequestevent.ExpiresAtLT(cutoff)).
		Order(ent.Asc(entmonitorrequestevent.FieldExpiresAt), ent.Asc(entmonitorrequestevent.FieldID)).
		Limit(batchSize).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return s.db.MonitorRequestEvent.Delete().
		Where(entmonitorrequestevent.IDIn(ids...)).
		Exec(ctx)
}

func applyMonitorRequestListFilter(query *ent.MonitorRequestEventQuery, filter appmonitor.RequestListFilter) *ent.MonitorRequestEventQuery {
	if filter.Severity != "" {
		query = query.Where(entmonitorrequestevent.SeverityEQ(entmonitorrequestevent.Severity(filter.Severity)))
	}
	if filter.Type != "" {
		query = query.Where(entmonitorrequestevent.TypeEQ(filter.Type))
	}
	if filter.Source != "" {
		query = query.Where(entmonitorrequestevent.SourceEQ(filter.Source))
	}
	if filter.APIKeyID != nil {
		query = query.Where(entmonitorrequestevent.APIKeyIDEQ(*filter.APIKeyID))
	}
	if filter.GroupID != nil {
		query = query.Where(entmonitorrequestevent.GroupIDEQ(*filter.GroupID))
	}
	if filter.AccountID != nil {
		query = query.Where(entmonitorrequestevent.AccountIDEQ(*filter.AccountID))
	}
	if filter.Platform != "" {
		query = query.Where(entmonitorrequestevent.PlatformEQ(filter.Platform))
	}
	if filter.PluginID != "" {
		query = query.Where(entmonitorrequestevent.PluginIDEQ(filter.PluginID))
	}
	if filter.Method != "" {
		query = query.Where(entmonitorrequestevent.MethodEQ(filter.Method))
	}
	if filter.Endpoint != "" {
		query = query.Where(entmonitorrequestevent.EndpointEQ(filter.Endpoint))
	}
	if filter.Model != "" {
		query = query.Where(entmonitorrequestevent.ModelEQ(filter.Model))
	}
	if filter.HTTPStatus != nil {
		query = query.Where(entmonitorrequestevent.HTTPStatusEQ(*filter.HTTPStatus))
	}
	if filter.UpstreamStatus != nil {
		query = query.Where(entmonitorrequestevent.UpstreamStatusEQ(*filter.UpstreamStatus))
	}
	if filter.ErrorCode != "" {
		query = query.Where(entmonitorrequestevent.ErrorCodeEQ(filter.ErrorCode))
	}
	if filter.From != nil {
		query = query.Where(entmonitorrequestevent.CreatedAtGTE(*filter.From))
	}
	if filter.To != nil {
		query = query.Where(entmonitorrequestevent.CreatedAtLTE(*filter.To))
	}
	return query
}

func setMonitorRequestCreateFields(create *ent.MonitorRequestEventCreate, event appmonitor.QueuedRequestEvent) *ent.MonitorRequestEventCreate {
	return create.
		SetType(event.Type).
		SetSeverity(entmonitorrequestevent.Severity(event.Severity)).
		SetSource(event.Source).
		SetHash(event.Hash).
		SetFingerprint(event.Fingerprint).
		SetTitle(event.Title).
		SetMessage(event.Message).
		SetRequestID(event.RequestID).
		SetNillableAPIKeyID(event.APIKeyID).
		SetAPIKeyNameSnapshot(event.APIKeyNameSnapshot).
		SetNillableUserID(event.UserID).
		SetUserEmailSnapshot(event.UserEmailSnapshot).
		SetNillableGroupID(event.GroupID).
		SetNillableAccountID(event.AccountID).
		SetAccountNameSnapshot(event.AccountNameSnapshot).
		SetPlatform(event.Platform).
		SetPluginID(event.PluginID).
		SetMethod(event.Method).
		SetEndpoint(event.Endpoint).
		SetModel(event.Model).
		SetNillableHTTPStatus(event.HTTPStatus).
		SetNillableUpstreamStatus(event.UpstreamStatus).
		SetErrorCode(event.ErrorCode).
		SetDurationMs(event.DurationMS).
		SetCreatedAt(event.CreatedAt).
		SetExpiresAt(event.ExpiresAt).
		SetDetail(event.Detail)
}

func mapMonitorRequestEvent(row *ent.MonitorRequestEvent) appmonitor.RequestEvent {
	if row == nil {
		return appmonitor.RequestEvent{}
	}
	detail := row.Detail
	if detail == nil {
		detail = map[string]interface{}{}
	}
	return appmonitor.RequestEvent{
		ID:                  row.ID,
		Type:                row.Type,
		Severity:            string(row.Severity),
		Source:              row.Source,
		Hash:                row.Hash,
		Fingerprint:         row.Fingerprint,
		Title:               row.Title,
		Message:             row.Message,
		RequestID:           row.RequestID,
		APIKeyID:            row.APIKeyID,
		APIKeyNameSnapshot:  row.APIKeyNameSnapshot,
		UserID:              row.UserID,
		UserEmailSnapshot:   row.UserEmailSnapshot,
		GroupID:             row.GroupID,
		AccountID:           row.AccountID,
		AccountNameSnapshot: row.AccountNameSnapshot,
		Platform:            row.Platform,
		PluginID:            row.PluginID,
		Method:              row.Method,
		Endpoint:            row.Endpoint,
		Model:               row.Model,
		HTTPStatus:          row.HTTPStatus,
		UpstreamStatus:      row.UpstreamStatus,
		ErrorCode:           row.ErrorCode,
		DurationMS:          row.DurationMs,
		CreatedAt:           row.CreatedAt,
		ExpiresAt:           row.ExpiresAt,
		Detail:              detail,
	}
}
