package bootstrap

import (
	"context"
	"crypto/sha256"
	stdsql "database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/apikey"
	"github.com/DouDOU-start/airgate-core/internal/auth"
)

//go:embed migrations/*.sql
var startupMigrationFiles embed.FS

// RunStartupTasks 运行启动阶段的整理任务。
func RunStartupTasks(db *ent.Client, drv *entsql.Driver, apiKeySecret string) {
	slog.Info("bootstrap_startup_tasks_start")
	backfillKeyHints(db, apiKeySecret)
	backfillResellerMarkupColumns(drv)
	migrateAccountState(drv)
	migrateUserHistoryRefs(drv)
	runStartupMigrations(drv)
	slog.Info("bootstrap_startup_tasks_done")
}

type startupMigration struct {
	ID          string
	Description string
	Checksum    string
	SQL         string
}

func runStartupMigrations(drv *entsql.Driver) {
	if drv == nil {
		return
	}
	migrations := loadStartupMigrations()
	if len(migrations) == 0 {
		return
	}

	ctx := context.Background()
	conn, err := drv.DB().Conn(ctx)
	if err != nil {
		panicStartupMigration("open startup migration connection", err)
	}
	defer func() { _ = conn.Close() }()

	const createTableSQL = `CREATE TABLE IF NOT EXISTS public.bootstrap_migrations (
		id text PRIMARY KEY,
		description text NOT NULL DEFAULT '',
		checksum text NOT NULL DEFAULT '',
		applied_at timestamptz NOT NULL DEFAULT now(),
		duration_ms bigint NOT NULL DEFAULT 0
	)`
	if _, err := conn.ExecContext(ctx, createTableSQL); err != nil {
		panicStartupMigration("create bootstrap_migrations table", err)
	}
	if _, err := conn.ExecContext(ctx, `ALTER TABLE public.bootstrap_migrations ADD COLUMN IF NOT EXISTS checksum text NOT NULL DEFAULT ''`); err != nil {
		panicStartupMigration("add bootstrap_migrations checksum column", err)
	}

	const lockKey int64 = 2026052809517
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		panicStartupMigration("lock startup migrations", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
			slog.Warn("bootstrap_migration_unlock_failed", sdk.LogFieldError, err)
		}
	}()

	for _, migration := range migrations {
		var appliedChecksum stdsql.NullString
		const appliedSQL = `SELECT checksum FROM public.bootstrap_migrations WHERE id = $1`
		err := conn.QueryRowContext(ctx, appliedSQL, migration.ID).Scan(&appliedChecksum)
		if err == nil {
			if appliedChecksum.Valid && appliedChecksum.String != "" && appliedChecksum.String != migration.Checksum {
				panicStartupMigration("verify startup migration checksum "+migration.ID, fmt.Errorf("recorded=%s current=%s", appliedChecksum.String, migration.Checksum))
			}
			if !appliedChecksum.Valid || appliedChecksum.String == "" {
				const updateSQL = `UPDATE public.bootstrap_migrations
					SET checksum = $2, description = $3
					WHERE id = $1 AND checksum = ''`
				if _, err := conn.ExecContext(ctx, updateSQL, migration.ID, migration.Checksum, migration.Description); err != nil {
					panicStartupMigration("backfill startup migration checksum "+migration.ID, err)
				}
			}
			continue
		}
		if err != stdsql.ErrNoRows {
			panicStartupMigration("check startup migration "+migration.ID, err)
		}

		start := time.Now()
		slog.Info("bootstrap_migration_start", "id", migration.ID)
		if err := executeStartupMigrationSQL(ctx, conn, migration); err != nil {
			panicStartupMigration("run startup migration "+migration.ID, err)
		}
		duration := time.Since(start).Milliseconds()
		const insertSQL = `INSERT INTO public.bootstrap_migrations (id, description, checksum, duration_ms)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (id) DO NOTHING`
		if _, err := conn.ExecContext(ctx, insertSQL, migration.ID, migration.Description, migration.Checksum, duration); err != nil {
			panicStartupMigration("record startup migration "+migration.ID, err)
		}
		slog.Info("bootstrap_migration_done", "id", migration.ID, "duration_ms", duration)
	}
}

func loadStartupMigrations() []startupMigration {
	entries, err := fs.ReadDir(startupMigrationFiles, "migrations")
	if err != nil {
		panicStartupMigration("read startup migration files", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	migrations := make([]startupMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := filepath.ToSlash(filepath.Join("migrations", entry.Name()))
		data, err := startupMigrationFiles.ReadFile(path)
		if err != nil {
			panicStartupMigration("read startup migration "+entry.Name(), err)
		}
		hash := sha256.Sum256(data)
		id := strings.TrimSuffix(entry.Name(), ".sql")
		sql := string(data)
		migrations = append(migrations, startupMigration{
			ID:          id,
			Description: startupMigrationDescription(sql, id),
			Checksum:    hex.EncodeToString(hash[:]),
			SQL:         sql,
		})
	}
	return migrations
}

func startupMigrationDescription(sql, fallback string) string {
	for _, line := range strings.Split(sql, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-- description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "-- description:"))
		}
	}
	return fallback
}

func executeStartupMigrationSQL(ctx context.Context, conn *stdsql.Conn, migration startupMigration) error {
	for _, stmt := range splitSQLStatements(migration.SQL) {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("execute statement in %s: %w", migration.ID, err)
		}
	}
	return nil
}

func splitSQLStatements(sql string) []string {
	var statements []string
	start := 0
	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	inBlockComment := false
	dollarTag := ""

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		next := byte(0)
		if i+1 < len(sql) {
			next = sql[i+1]
		}

		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
			}
			continue
		case inBlockComment:
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		case dollarTag != "":
			if strings.HasPrefix(sql[i:], dollarTag) {
				i += len(dollarTag) - 1
				dollarTag = ""
			}
			continue
		case inSingleQuote:
			if ch == '\'' {
				if next == '\'' {
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		case inDoubleQuote:
			if ch == '"' {
				if next == '"' {
					i++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		if ch == '-' && next == '-' {
			inLineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}
		if ch == '\'' {
			inSingleQuote = true
			continue
		}
		if ch == '"' {
			inDoubleQuote = true
			continue
		}
		if ch == '$' {
			if tag, ok := sqlDollarTag(sql[i:]); ok {
				dollarTag = tag
				i += len(tag) - 1
				continue
			}
		}
		if ch == ';' {
			stmt := strings.TrimSpace(sql[start:i])
			if stmt != "" {
				statements = append(statements, stmt)
			}
			start = i + 1
		}
	}

	tail := strings.TrimSpace(sql[start:])
	if tail != "" {
		statements = append(statements, tail)
	}
	return statements
}

func sqlDollarTag(sql string) (string, bool) {
	if sql == "" || sql[0] != '$' {
		return "", false
	}
	for i := 1; i < len(sql); i++ {
		ch := sql[i]
		if ch == '$' {
			return sql[:i+1], true
		}
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return "", false
		}
	}
	return "", false
}

func panicStartupMigration(action string, err error) {
	if err != nil {
		err = fmt.Errorf("%s: %w", action, err)
	} else {
		err = fmt.Errorf("%s", action)
	}
	slog.Error("bootstrap_startup_migration_failed", sdk.LogFieldError, err)
	panic(err)
}

// migrateUserHistoryRefs 允许硬删除用户，同时保留历史使用记录和余额流水。
// 用量/计费聚合依赖 usage_logs 的成本快照字段；这里把历史表的 user 外键改为 SET NULL，
// 并回填 user_id/user_email 快照，避免删除用户后历史记录丢失归属信息。
func migrateUserHistoryRefs(drv *entsql.Driver) {
	if drv == nil {
		return
	}
	ctx := context.Background()
	statements := []string{
		`ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS user_id_snapshot integer NOT NULL DEFAULT 0`,
		`ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS user_email_snapshot text NOT NULL DEFAULT ''`,
		`ALTER TABLE balance_logs ADD COLUMN IF NOT EXISTS user_id_snapshot integer NOT NULL DEFAULT 0`,
		`ALTER TABLE balance_logs ADD COLUMN IF NOT EXISTS user_email_snapshot text NOT NULL DEFAULT ''`,
	}

	usageFKReady, ok := historyUserRefReady(ctx, drv, "usage_logs", "user_usage_logs", "usage_logs_users_usage_logs")
	if !ok {
		return
	}
	if !usageFKReady {
		statements = append(statements,
			`UPDATE usage_logs AS ul
			SET user_id_snapshot = CASE WHEN ul.user_id_snapshot = 0 THEN u.id ELSE ul.user_id_snapshot END,
				user_email_snapshot = CASE WHEN ul.user_email_snapshot = '' THEN u.email ELSE ul.user_email_snapshot END
			FROM users AS u
			WHERE ul.user_usage_logs = u.id
				AND (ul.user_id_snapshot = 0 OR ul.user_email_snapshot = '')`,
			`ALTER TABLE usage_logs ALTER COLUMN user_usage_logs DROP NOT NULL`,
			`ALTER TABLE usage_logs DROP CONSTRAINT IF EXISTS usage_logs_users_usage_logs`,
			`ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_users_usage_logs
			FOREIGN KEY (user_usage_logs) REFERENCES users(id) ON DELETE SET NULL NOT VALID`,
		)
	}

	balanceFKReady, ok := historyUserRefReady(ctx, drv, "balance_logs", "user_balance_logs", "balance_logs_users_balance_logs")
	if !ok {
		return
	}
	if !balanceFKReady {
		statements = append(statements,
			`UPDATE balance_logs AS bl
			SET user_id_snapshot = CASE WHEN bl.user_id_snapshot = 0 THEN u.id ELSE bl.user_id_snapshot END,
				user_email_snapshot = CASE WHEN bl.user_email_snapshot = '' THEN u.email ELSE bl.user_email_snapshot END
			FROM users AS u
			WHERE bl.user_balance_logs = u.id
				AND (bl.user_id_snapshot = 0 OR bl.user_email_snapshot = '')`,
			`ALTER TABLE balance_logs ALTER COLUMN user_balance_logs DROP NOT NULL`,
			`ALTER TABLE balance_logs DROP CONSTRAINT IF EXISTS balance_logs_users_balance_logs`,
			`ALTER TABLE balance_logs ADD CONSTRAINT balance_logs_users_balance_logs
			FOREIGN KEY (user_balance_logs) REFERENCES users(id) ON DELETE SET NULL NOT VALID`,
		)
	}

	for _, sql := range statements {
		var r entsql.Result
		if err := drv.Exec(ctx, sql, []any{}, &r); err != nil {
			slog.Warn("bootstrap_user_history_refs_migration_failed", "sql", sql, sdk.LogFieldError, err)
			return
		}
	}
}

func historyUserRefReady(ctx context.Context, drv *entsql.Driver, table, column, constraint string) (bool, bool) {
	nullable, ok := tableColumnNullable(ctx, drv, table, column)
	if !ok {
		return false, false
	}
	if !nullable {
		return false, true
	}
	return tableForeignKeyOnDeleteSetNull(ctx, drv, table, constraint)
}

func tableColumnNullable(ctx context.Context, drv *entsql.Driver, table, column string) (bool, bool) {
	var rows entsql.Rows
	const checkSQL = `SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
			AND table_name = $1
			AND column_name = $2
		LIMIT 1`
	if err := drv.Query(ctx, checkSQL, []any{table, column}, &rows); err != nil {
		slog.Warn("bootstrap_column_nullable_check_failed", "table", table, "column", column, sdk.LogFieldError, err)
		return false, false
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		slog.Warn("bootstrap_column_nullable_check_missing", "table", table, "column", column)
		return false, false
	}
	var nullable string
	if err := rows.Scan(&nullable); err != nil {
		slog.Warn("bootstrap_column_nullable_scan_failed", "table", table, "column", column, sdk.LogFieldError, err)
		return false, false
	}
	return nullable == "YES", true
}

func tableForeignKeyOnDeleteSetNull(ctx context.Context, drv *entsql.Driver, table, constraint string) (bool, bool) {
	var rows entsql.Rows
	const checkSQL = `SELECT 1
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = 'public'
			AND t.relname = $1
			AND c.conname = $2
			AND c.contype = 'f'
			AND c.confdeltype = 'n'
		LIMIT 1`
	if err := drv.Query(ctx, checkSQL, []any{table, constraint}, &rows); err != nil {
		slog.Warn("bootstrap_foreign_key_check_failed", "table", table, "constraint", constraint, sdk.LogFieldError, err)
		return false, false
	}
	defer func() { _ = rows.Close() }()
	return rows.Next(), true
}

// migrateAccountState 把老的 status / rate_limit_reset_at 字段一次性迁移到新的
// state / state_until，然后 DROP 旧列。幂等：首次启动或升级时有效，之后旧列已不存在时跳过。
//
// 映射规则：
//
//	status='error'    → state='disabled'
//	status='disabled' → state='disabled'
//	rate_limit_reset_at > now() → state='rate_limited', state_until = rate_limit_reset_at
//	其它               → state='active'（ent 默认值，不需要改）
func migrateAccountState(drv *entsql.Driver) {
	if drv == nil {
		return
	}
	ctx := context.Background()

	hasStatus, ok := accountColumnExists(ctx, drv, "status")
	if !ok {
		return
	}
	hasRateLimitResetAt, ok := accountColumnExists(ctx, drv, "rate_limit_reset_at")
	if !ok {
		return
	}
	if !hasStatus && !hasRateLimitResetAt {
		return
	}

	slog.Info("bootstrap_account_state_migration_start")

	updates := make([]string, 0, 2)
	if hasStatus || hasRateLimitResetAt {
		stateCase := []string{"state = CASE"}
		if hasStatus {
			stateCase = append(stateCase, "WHEN status IN ('error', 'disabled') THEN 'disabled'")
		}
		if hasRateLimitResetAt {
			stateCase = append(stateCase, "WHEN rate_limit_reset_at IS NOT NULL AND rate_limit_reset_at > NOW() THEN 'rate_limited'")
		}
		stateCase = append(stateCase, "ELSE 'active' END")
		updates = append(updates, strings.Join(stateCase, " "))
	}
	if hasRateLimitResetAt {
		updates = append(updates, `state_until = CASE
			WHEN rate_limit_reset_at IS NOT NULL AND rate_limit_reset_at > NOW() THEN rate_limit_reset_at
			ELSE NULL
		END`)
	}

	var res entsql.Result
	updateSQL := "UPDATE accounts SET " + strings.Join(updates, ", ")
	if err := drv.Exec(ctx, updateSQL, []any{}, &res); err != nil {
		slog.Error("bootstrap_account_state_migration_failed", sdk.LogFieldError, err)
		return
	}
	if affected, err := res.RowsAffected(); err == nil {
		slog.Info("bootstrap_account_state_migration_done", "rows", affected)
	}

	// 然后删旧列。WithDropColumn(false) 让 ent 不自动删，所以手工 DROP。
	drops := []string{
		`ALTER TABLE accounts DROP COLUMN IF EXISTS status`,
		`ALTER TABLE accounts DROP COLUMN IF EXISTS rate_limit_reset_at`,
	}
	for _, sql := range drops {
		var r entsql.Result
		if err := drv.Exec(ctx, sql, []any{}, &r); err != nil {
			slog.Warn("bootstrap_drop_legacy_column_failed", "sql", sql, sdk.LogFieldError, err)
		}
	}
	slog.Info("bootstrap_account_legacy_columns_dropped")
}

func accountColumnExists(ctx context.Context, drv *entsql.Driver, column string) (bool, bool) {
	return tableColumnExists(ctx, drv, "accounts", column)
}

func tableColumnExists(ctx context.Context, drv *entsql.Driver, table, column string) (bool, bool) {
	var exists entsql.Rows
	const checkSQL = `SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public'
			AND table_name=$1
			AND column_name=$2
		LIMIT 1`
	if err := drv.Query(ctx, checkSQL, []any{table, column}, &exists); err != nil {
		slog.Warn("bootstrap_column_check_failed", "table", table, "column", column, sdk.LogFieldError, err)
		return false, false
	}
	defer func() { _ = exists.Close() }()
	return exists.Next(), true
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
			slog.Warn("bootstrap_reseller_backfill_failed", "table", stmt.label, sdk.LogFieldError, err)
			continue
		}
		if affected, err := res.RowsAffected(); err == nil && affected > 0 {
			slog.Info("bootstrap_reseller_backfill_done", "table", stmt.label, "rows", affected)
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
		slog.Warn("bootstrap_keyhint_query_failed", sdk.LogFieldError, err)
		return
	}
	if len(keys) == 0 {
		return
	}

	slog.Info("bootstrap_keyhint_backfill_start", "count", len(keys))
	for _, item := range keys {
		if item.KeyEncrypted == "" {
			continue
		}
		plain, err := auth.DecryptAPIKey(item.KeyEncrypted, secret)
		if err != nil {
			slog.Warn("bootstrap_keyhint_decrypt_failed", sdk.LogFieldAPIKeyID, item.ID, sdk.LogFieldError, err)
			continue
		}
		hint := plain[:7] + "..." + plain[len(plain)-4:]
		if err := db.APIKey.UpdateOneID(item.ID).SetKeyHint(hint).Exec(ctx); err != nil {
			slog.Warn("bootstrap_keyhint_update_failed", sdk.LogFieldAPIKeyID, item.ID, sdk.LogFieldError, err)
		}
	}
	slog.Info("bootstrap_keyhint_backfill_done", "count", len(keys))
}
