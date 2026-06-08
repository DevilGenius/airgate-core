package store

import (
	"context"
	"time"

	"entgo.io/ent/dialect/sql"

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

func usageLogColumnIn(column string, values []int) predicate.UsageLog {
	return predicate.UsageLog(func(s *sql.Selector) {
		s.Where(sql.InInts(s.C(column), values...))
	})
}
