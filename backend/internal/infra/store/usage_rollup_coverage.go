package store

import (
	"context"
	stdsql "database/sql"
	"errors"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/lib/pq"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/reporting"
)

// A nil/zero lower bound requests all history and requires a verified backfill.
// Failure is explicit: an empty/partially populated table is never a zero total.
func requireUsageRollupCoverage(ctx context.Context, db *ent.Client, projection string, from time.Time) error {
	query, args := sql.Dialect(db.Driver().Dialect()).Select("version", "state", "covered_from").From(sql.Table("usage_rollup_coverage")).Where(sql.EQ("projection", strings.TrimPrefix(projection, "public."))).Query()
	var rows sql.Rows
	if err := db.Driver().Query(ctx, query, args, &rows); err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return reporting.ErrHistoryNotReady
		}
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return reporting.ErrHistoryNotReady
	}
	var version int
	var state string
	var coveredFrom stdsql.NullTime
	if err := rows.Scan(&version, &state, &coveredFrom); err != nil {
		return err
	}
	if state == "failed" {
		return reporting.ErrHistoryNeedsBackfill
	}
	if version != 1 {
		return reporting.ErrHistoryNotReady
	}
	if state == "ready" || (!from.IsZero() && coveredFrom.Valid && !from.Before(coveredFrom.Time)) {
		return nil
	}
	return reporting.ErrHistoryNotReady
}
