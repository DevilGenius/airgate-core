package store

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
)

// Collapse credentials/models before ranking keys. Names and timezone conversion
// are applied only to the selected keys' buckets, not every source rollup row.
const dashboardAPIKeyTrendQuery = `
WITH buckets AS MATERIALIZED (
	SELECT api_key_id, bucket_start,
		SUM(requests)::bigint AS requests,
		SUM(input_tokens + output_tokens + cached_input_tokens + cache_creation_tokens)::bigint AS tokens,
		SUM(billed_cost) AS billed_cost
	FROM public.usage_api_key_hourly_rollups
	WHERE bucket_start >= $1 AND bucket_start < $2
		AND ($3::integer = 0 OR user_id = $3::integer)
	GROUP BY api_key_id, bucket_start
), top_keys AS (
	SELECT api_key_id FROM buckets
	GROUP BY api_key_id
	ORDER BY SUM(tokens) DESC, api_key_id
	LIMIT 12
), selected_buckets AS (
	SELECT b.api_key_id, MIN(b.bucket_start) AS bucket_start,
		SUM(b.requests)::bigint AS requests,
		SUM(b.tokens)::bigint AS tokens,
		SUM(b.billed_cost)::double precision AS billed_cost
	FROM buckets b JOIN top_keys t USING (api_key_id)
	GROUP BY b.api_key_id, CASE WHEN $4::text = 'day'
		THEN date_trunc('day', b.bucket_start AT TIME ZONE $5::text)
		ELSE b.bucket_start AT TIME ZONE 'UTC' END
)
SELECT b.api_key_id, COALESCE(k.name, ''), b.requests, b.tokens, b.billed_cost, b.bucket_start
FROM selected_buckets b
LEFT JOIN public.api_keys k ON k.id = b.api_key_id
ORDER BY b.bucket_start, b.api_key_id`

// ListAPIKeyTrendLogs reads only Top12 buckets from the hourly projection.
// No raw usage-log fallback is allowed, including for incomplete history.
func (s *DashboardStore) ListAPIKeyTrendLogs(ctx context.Context, startTime, endTime time.Time, userID int, granularity string, loc *time.Location) ([]appdashboard.APIKeyTrendLog, error) {
	if !s.canQueryDashboardRollups() {
		return []appdashboard.APIKeyTrendLog{}, nil
	}
	if err := requireUsageRollupCoverage(ctx, s.db, usageAPIKeyHourlyRollupTable, startTime); err != nil {
		return nil, err
	}
	zone := loc.String()
	if zone == "Local" {
		// PostgreSQL cannot resolve Go's OS-specific local zone. Keep hour buckets;
		// the service folds these into local days with the actual Go location.
		granularity, zone = "hour", "UTC"
	}
	var rows entsql.Rows
	if err := s.db.Driver().Query(ctx, dashboardAPIKeyTrendQuery, []any{startTime, endTime, userID, granularity, zone}, &rows); err != nil {
		if isDashboardRollupUnavailable(err) {
			return []appdashboard.APIKeyTrendLog{}, nil
		}
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]appdashboard.APIKeyTrendLog, 0)
	for rows.Next() {
		var item appdashboard.APIKeyTrendLog
		if err := rows.Scan(&item.APIKeyID, &item.APIKeyName, &item.Requests, &item.Tokens, &item.BilledCost, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
