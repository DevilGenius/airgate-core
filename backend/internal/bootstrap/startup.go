package bootstrap

import (
	"context"
	"log/slog"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/apikey"
	"github.com/DouDOU-start/airgate-core/internal/auth"
)

// RunStartupTasks 运行启动阶段的整理任务。
func RunStartupTasks(db *ent.Client, drv *entsql.Driver, apiKeySecret string) {
	backfillKeyHints(db, apiKeySecret)
	backfillResellerMarkupColumns(drv)
}

// backfillResellerMarkupColumns 一次性回填 reseller markup 改造引入的两个新列：
//   - usage_logs.billed_cost：历史行未启用 markup，账面 = 真实成本
//   - api_keys.used_quota_actual：历史 key 未启用 markup，actual 累加值 = used_quota
//
// SQL 使用 idempotent 条件 WHERE billed_cost = 0 / used_quota_actual = 0，
// 多次启动重复执行也不会污染已经被新代码正确写入的数据。
func backfillResellerMarkupColumns(drv *entsql.Driver) {
	if drv == nil {
		return
	}
	ctx := context.Background()

	statements := []struct {
		label string
		sql   string
	}{
		{"usage_logs.billed_cost", "UPDATE usage_logs SET billed_cost = actual_cost WHERE billed_cost = 0 AND actual_cost > 0"},
		// 历史 account_rate 全是 1.0，account_cost 等价 total_cost
		{"usage_logs.account_cost", "UPDATE usage_logs SET account_cost = total_cost WHERE account_cost = 0 AND total_cost > 0"},
		{"api_keys.used_quota_actual", "UPDATE api_keys SET used_quota_actual = used_quota WHERE used_quota_actual = 0 AND used_quota > 0"},
	}

	for _, stmt := range statements {
		var res entsql.Result
		if err := drv.Exec(ctx, stmt.sql, []any{}, &res); err != nil {
			slog.Warn("回填 reseller markup 列失败（可忽略，仅影响历史展示）", "table", stmt.label, "error", err)
			continue
		}
		if affected, err := res.RowsAffected(); err == nil && affected > 0 {
			slog.Info("回填 reseller markup 列", "table", stmt.label, "rows", affected)
		}
	}
}

// backfillKeyHints 为缺少或格式过旧的 key_hint 回填 sk-xxxx...xxxx。
func backfillKeyHints(db *ent.Client, secret string) {
	ctx := context.Background()
	keys, err := db.APIKey.Query().
		Where(apikey.Or(
			apikey.KeyHint(""),
			apikey.KeyHintHasPrefix("sk-..."),
		)).
		All(ctx)
	if err != nil {
		slog.Warn("查询待回填 API Key 失败", "error", err)
		return
	}
	if len(keys) == 0 {
		return
	}

	slog.Info("回填 API Key hint", "count", len(keys))
	for _, item := range keys {
		if item.KeyEncrypted == "" {
			continue
		}
		plain, err := auth.DecryptAPIKey(item.KeyEncrypted, secret)
		if err != nil {
			slog.Warn("解密 API Key 失败，跳过", "id", item.ID, "error", err)
			continue
		}
		hint := plain[:7] + "..." + plain[len(plain)-4:]
		if err := db.APIKey.UpdateOneID(item.ID).SetKeyHint(hint).Exec(ctx); err != nil {
			slog.Warn("回填 key_hint 失败", "id", item.ID, "error", err)
		}
	}
	slog.Info("API Key hint 回填完成")
}
