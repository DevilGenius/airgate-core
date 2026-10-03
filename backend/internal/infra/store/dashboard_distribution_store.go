package store

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
)

// Aggregate both dimensions in one pass over the time-bounded hourly projection.
// Join display names only after aggregation; never read raw usage_logs here.
const dashboardDistributionQuery = `
WITH distribution AS (
	SELECT
		CASE WHEN GROUPING(account_id) = 0 THEN 'account' ELSE 'group' END AS dimension,
		CASE WHEN GROUPING(account_id) = 0 THEN account_id ELSE group_id END AS id,
		COALESCE(SUM(requests), 0)::bigint AS requests,
		COALESCE(SUM(input_tokens + output_tokens + cached_input_tokens + cache_creation_tokens), 0)::bigint AS tokens,
		COALESCE(SUM(actual_cost), 0)::double precision AS actual_cost,
		COALESCE(SUM(total_cost), 0)::double precision AS standard_cost
	FROM public.usage_api_key_hourly_rollups
	WHERE bucket_start >= $1
		AND bucket_start < $2
		AND ($3::integer = 0 OR user_id = $3::integer)
	GROUP BY GROUPING SETS ((account_id), (group_id))
)
SELECT
	d.dimension,
	d.id,
	CASE WHEN d.dimension = 'account' THEN COALESCE(a.name, '') ELSE COALESCE(g.name, '') END,
	d.requests,
	d.tokens,
	d.actual_cost,
	d.standard_cost
FROM distribution d
LEFT JOIN public.accounts a ON d.dimension = 'account' AND a.id = d.id
LEFT JOIN public.groups g ON d.dimension = 'group' AND g.id = d.id
ORDER BY d.dimension, d.requests DESC, d.id`

// LoadDistributionStats 沿用仪表盘小时汇总和历史覆盖校验，不回退扫描使用记录。
func (s *DashboardStore) LoadDistributionStats(ctx context.Context, startTime, endTime time.Time, userID int) (appdashboard.DistributionSnapshot, error) {
	result := appdashboard.DistributionSnapshot{
		Accounts: make([]appdashboard.DistributionStats, 0),
		Groups:   make([]appdashboard.DistributionStats, 0),
	}
	if !s.canQueryDashboardRollups() {
		return result, nil
	}
	if err := requireUsageRollupCoverage(ctx, s.db, usageAPIKeyHourlyRollupTable, startTime); err != nil {
		return appdashboard.DistributionSnapshot{}, err
	}
	var rows entsql.Rows
	if err := s.db.Driver().Query(ctx, dashboardDistributionQuery, []any{startTime, endTime, userID}, &rows); err != nil {
		return appdashboard.DistributionSnapshot{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var dimension string
		var item appdashboard.DistributionStats
		if err := rows.Scan(&dimension, &item.ID, &item.Name, &item.Requests, &item.Tokens, &item.ActualCost, &item.StandardCost); err != nil {
			return appdashboard.DistributionSnapshot{}, err
		}
		switch dimension {
		case "account":
			result.Accounts = append(result.Accounts, item)
		case "group":
			result.Groups = append(result.Groups, item)
		}
	}
	if err := rows.Err(); err != nil {
		return appdashboard.DistributionSnapshot{}, err
	}
	return result, nil
}
