package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/lib/pq"

	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
	"github.com/DevilGenius/airgate-core/internal/pkg/timezone"
)

const usageAPIKeyHourlyRollupTable = "public.usage_api_key_hourly_rollups"

func (s *UsageStore) summaryAdminFromAPIKeyRollups(ctx context.Context, filter appusage.StatsFilter) (appusage.Summary, bool, error) {
	if s == nil || s.db == nil || s.db.Driver() == nil || s.db.Driver().Dialect() != dialect.Postgres || filter.ScopedToKey {
		return appusage.Summary{}, false, nil
	}

	where := make([]string, 0, 12)
	args := make([]any, 0, 12)
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.UserID != nil {
		where = append(where, "r.user_id = "+addArg(*filter.UserID)+"::bigint")
	}
	if filter.APIKeyID != nil {
		where = append(where, "r.api_key_id = "+addArg(*filter.APIKeyID)+"::bigint")
	}
	if filter.Platform != "" {
		where = append(where, "r.platform = "+addArg(filter.Platform)+"::text")
	}
	includeModels, excludeModels := appusage.ParseModelFilter(filter.Model)
	if len(includeModels) > 0 {
		parts := make([]string, 0, len(includeModels))
		for _, model := range includeModels {
			parts = append(parts, "r.model LIKE "+addArg("%"+model+"%")+"::text")
		}
		where = append(where, "("+strings.Join(parts, " OR ")+")")
	}
	if len(excludeModels) > 0 {
		parts := make([]string, 0, len(excludeModels))
		for _, model := range excludeModels {
			parts = append(parts, "r.model LIKE "+addArg("%"+model+"%")+"::text")
		}
		where = append(where, "NOT ("+strings.Join(parts, " OR ")+")")
	}
	if accountSearch := strings.TrimSpace(filter.AccountSearch); accountSearch != "" {
		pattern := "%" + accountSearch + "%"
		where = append(where, `EXISTS (
			SELECT 1 FROM public.accounts a
			WHERE a.id = r.account_id
				AND (a.name ILIKE `+addArg(pattern)+`::text OR COALESCE(a.email, '') ILIKE `+addArg(pattern)+`::text)
		)`)
	}
	loc := timezone.Resolve(filter.TZ)
	if filter.StartDate != "" {
		if start, err := timezone.ParseDate(filter.StartDate, loc); err == nil {
			if !hourBoundary(start) {
				return appusage.Summary{}, false, nil
			}
			where = append(where, "r.bucket_start >= "+addArg(start)+"::timestamptz")
		}
	}
	if filter.EndDate != "" {
		if end, err := timezone.ParseDate(filter.EndDate, loc); err == nil {
			if !hourBoundary(end) {
				return appusage.Summary{}, false, nil
			}
			where = append(where, "r.bucket_start < "+addArg(end.AddDate(0, 0, 1))+"::timestamptz")
		}
	}

	query := `SELECT
	COUNT(*)::bigint,
	COALESCE(SUM(r.requests), 0)::bigint,
	COALESCE(SUM(r.input_tokens + r.output_tokens + r.cached_input_tokens + r.cache_creation_tokens), 0)::bigint,
	COALESCE(SUM(r.total_cost), 0)::double precision,
	COALESCE(SUM(r.actual_cost), 0)::double precision,
	COALESCE(SUM(r.billed_cost), 0)::double precision
FROM ` + usageAPIKeyHourlyRollupTable + ` r`
	if len(where) > 0 {
		query += "\nWHERE " + strings.Join(where, "\n  AND ")
	}

	var rows entsql.Rows
	if err := s.db.Driver().Query(ctx, query, args, &rows); err != nil {
		if isUsageAPIKeyRollupUnavailable(err) {
			return appusage.Summary{}, false, nil
		}
		return appusage.Summary{}, true, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return appusage.Summary{}, true, err
		}
		return appusage.Summary{}, true, nil
	}
	var rollupRows int64
	var summary appusage.Summary
	if err := rows.Scan(
		&rollupRows,
		&summary.TotalRequests,
		&summary.TotalTokens,
		&summary.TotalCost,
		&summary.TotalActualCost,
		&summary.TotalBilledCost,
	); err != nil {
		return appusage.Summary{}, true, err
	}
	if err := rows.Err(); err != nil {
		return appusage.Summary{}, true, err
	}
	return summary, true, nil
}

func hourBoundary(value time.Time) bool {
	return value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0
}

func isUsageAPIKeyRollupUnavailable(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "42P01"
}
