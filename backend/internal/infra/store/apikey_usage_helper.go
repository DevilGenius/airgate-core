package store

import (
	"context"
	"errors"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/lib/pq"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/predicate"
	entusagelog "github.com/DevilGenius/airgate-core/ent/usagelog"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
)

// queryAPIKeyUsage 返回每个 key 的"今日"和"近 30 天"销售/消耗金额。
// todayStart 必须由调用方按用户时区计算好；近 30 天窗口以 todayStart 为锚。
func queryAPIKeyUsage(ctx context.Context, db *ent.Client, keyIDs []int, todayStart time.Time) (map[int]appapikey.UsageCosts, error) {
	usageMap := make(map[int]appapikey.UsageCosts, len(keyIDs))
	if len(keyIDs) == 0 {
		return usageMap, nil
	}
	if rollupUsage, handled, err := queryAPIKeyUsageFromRollups(ctx, db, keyIDs, todayStart); handled {
		return rollupUsage, err
	}

	thirtyDaysAgo := todayStart.AddDate(0, 0, -29)

	type costRow struct {
		APIKeyID   int     `json:"api_key_usage_logs"`
		SalesCost  float64 `json:"sales_cost"`
		ActualCost float64 `json:"actual_cost"`
	}

	var todayRows []costRow
	if err := db.UsageLog.Query().
		Where(
			usageLogColumnIn(entusagelog.APIKeyColumn, keyIDs),
			entusagelog.CreatedAtGTE(todayStart),
		).
		GroupBy(entusagelog.ForeignKeys[0]).
		Aggregate(
			ent.As(ent.Sum(entusagelog.FieldBilledCost), "sales_cost"),
			ent.As(ent.Sum(entusagelog.FieldActualCost), "actual_cost"),
		).
		Scan(ctx, &todayRows); err != nil {
		return nil, err
	}
	for _, row := range todayRows {
		costs := usageMap[row.APIKeyID]
		costs.TodaySalesCost = row.SalesCost
		costs.TodayActualCost = row.ActualCost
		usageMap[row.APIKeyID] = costs
	}

	var thirtyDayRows []costRow
	if err := db.UsageLog.Query().
		Where(
			usageLogColumnIn(entusagelog.APIKeyColumn, keyIDs),
			entusagelog.CreatedAtGTE(thirtyDaysAgo),
		).
		GroupBy(entusagelog.ForeignKeys[0]).
		Aggregate(
			ent.As(ent.Sum(entusagelog.FieldBilledCost), "sales_cost"),
			ent.As(ent.Sum(entusagelog.FieldActualCost), "actual_cost"),
		).
		Scan(ctx, &thirtyDayRows); err != nil {
		return nil, err
	}
	for _, row := range thirtyDayRows {
		costs := usageMap[row.APIKeyID]
		costs.ThirtyDaySalesCost = row.SalesCost
		costs.ThirtyDayActualCost = row.ActualCost
		usageMap[row.APIKeyID] = costs
	}

	return usageMap, nil
}

func queryAPIKeyUsageFromRollups(ctx context.Context, db *ent.Client, keyIDs []int, todayStart time.Time) (map[int]appapikey.UsageCosts, bool, error) {
	if db == nil || db.Driver() == nil || db.Driver().Dialect() != dialect.Postgres || !hourBoundary(todayStart) {
		return nil, false, nil
	}
	usageMap := make(map[int]appapikey.UsageCosts, len(keyIDs))
	thirtyDaysAgo := todayStart.AddDate(0, 0, -29)
	const query = `
SELECT
	api_key_id,
	COALESCE(SUM(billed_cost) FILTER (WHERE bucket_start >= $2), 0)::double precision,
	COALESCE(SUM(actual_cost) FILTER (WHERE bucket_start >= $2), 0)::double precision,
	COALESCE(SUM(billed_cost), 0)::double precision,
	COALESCE(SUM(actual_cost), 0)::double precision
FROM public.usage_api_key_hourly_rollups
WHERE api_key_id = ANY($1::integer[])
	AND bucket_start >= $3
GROUP BY api_key_id`
	var rows sql.Rows
	if err := db.Driver().Query(ctx, query, []any{pq.Array(keyIDs), todayStart, thirtyDaysAgo}, &rows); err != nil {
		if isAPIKeyRollupUnavailable(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var keyID int
		var costs appapikey.UsageCosts
		if err := rows.Scan(&keyID, &costs.TodaySalesCost, &costs.TodayActualCost, &costs.ThirtyDaySalesCost, &costs.ThirtyDayActualCost); err != nil {
			return nil, true, err
		}
		usageMap[keyID] = costs
	}
	if err := rows.Err(); err != nil {
		return nil, true, err
	}
	return usageMap, true, nil
}

func isAPIKeyRollupUnavailable(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "42P01"
}

func usageLogColumnIn(column string, values []int) predicate.UsageLog {
	return predicate.UsageLog(func(s *sql.Selector) {
		s.Where(sql.InInts(s.C(column), values...))
	})
}
