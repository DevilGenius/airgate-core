package store

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/monitorrequesttrace"
	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
)

// UpsertRequestTrace stores one content-addressed payload and appends the
// occurrence event in the same transaction.
func (s *MonitorStore) UpsertRequestTrace(ctx context.Context, trace appmonitor.StoredRequestTrace, event appmonitor.QueuedRequestEvent) error {
	if s == nil || s.db == nil || trace.Hash == "" {
		return nil
	}
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	insert := entsql.Dialect(tx.Driver().Dialect()).
		Insert(monitorrequesttrace.Table).
		Columns(
			monitorrequesttrace.FieldHash,
			monitorrequesttrace.FieldSchemaVersion,
			monitorrequesttrace.FieldEncoding,
			monitorrequesttrace.FieldPayload,
			monitorrequesttrace.FieldRawSize,
			monitorrequesttrace.FieldCompressedSize,
			monitorrequesttrace.FieldSeenCount,
			monitorrequesttrace.FieldFirstSeenAt,
			monitorrequesttrace.FieldLastSeenAt,
			monitorrequesttrace.FieldExpiresAt,
		).
		Values(
			trace.Hash,
			trace.SchemaVersion,
			trace.Encoding,
			trace.Payload,
			trace.RawSize,
			trace.CompressedSize,
			trace.SeenCount,
			trace.FirstSeenAt,
			trace.LastSeenAt,
			trace.ExpiresAt,
		).
		OnConflict(
			entsql.ConflictColumns(monitorrequesttrace.FieldHash),
			entsql.ResolveWith(func(update *entsql.UpdateSet) {
				update.
					Add(monitorrequesttrace.FieldSeenCount, 1).
					SetExcluded(monitorrequesttrace.FieldLastSeenAt).
					SetExcluded(monitorrequesttrace.FieldExpiresAt)
			}),
		)
	query, args := insert.Query()
	var result entsql.Result
	if err := tx.Driver().Exec(ctx, query, args, &result); err != nil {
		return err
	}
	if event.Hash != "" {
		if _, err := setMonitorRequestCreateFields(tx.MonitorRequestEvent.Create(), event).Save(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *MonitorStore) GetRequestTrace(ctx context.Context, hash string) (appmonitor.StoredRequestTrace, error) {
	if s == nil || s.db == nil || hash == "" {
		return appmonitor.StoredRequestTrace{}, appmonitor.ErrRequestTraceNotFound
	}
	row, err := s.db.MonitorRequestTrace.Query().
		Where(monitorrequesttrace.HashEQ(hash)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return appmonitor.StoredRequestTrace{}, appmonitor.ErrRequestTraceNotFound
	}
	if err != nil {
		return appmonitor.StoredRequestTrace{}, err
	}
	return mapStoredRequestTrace(row), nil
}

func (s *MonitorStore) ClearRequestTraces(ctx context.Context, before *time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	delete := s.db.MonitorRequestTrace.Delete()
	if before != nil && !before.IsZero() {
		delete = delete.Where(monitorrequesttrace.LastSeenAtLT(*before))
	}
	return delete.Exec(ctx)
}

func (s *MonitorStore) CleanupExpiredRequestTraces(ctx context.Context, cutoff time.Time, batchSize int) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	ids, err := s.db.MonitorRequestTrace.Query().
		Where(monitorrequesttrace.ExpiresAtLT(cutoff)).
		Order(ent.Asc(monitorrequesttrace.FieldExpiresAt), ent.Asc(monitorrequesttrace.FieldID)).
		Limit(batchSize).
		IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return s.db.MonitorRequestTrace.Delete().
		Where(monitorrequesttrace.IDIn(ids...)).
		Exec(ctx)
}

func mapStoredRequestTrace(row *ent.MonitorRequestTrace) appmonitor.StoredRequestTrace {
	if row == nil {
		return appmonitor.StoredRequestTrace{}
	}
	return appmonitor.StoredRequestTrace{
		Hash:           row.Hash,
		SchemaVersion:  row.SchemaVersion,
		Encoding:       row.Encoding,
		Payload:        row.Payload,
		RawSize:        row.RawSize,
		CompressedSize: row.CompressedSize,
		SeenCount:      row.SeenCount,
		FirstSeenAt:    row.FirstSeenAt,
		LastSeenAt:     row.LastSeenAt,
		ExpiresAt:      row.ExpiresAt,
	}
}
