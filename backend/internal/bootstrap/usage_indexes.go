package bootstrap

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/lib/pq"

	"github.com/DevilGenius/airgate-core/ent/migrate"
)

// Secondary analytics indexes are optional for correctness. Keep uniqueness
// constraints in startup DDL, and build the large-table indexes concurrently
// after HTTP starts. Copy descriptors so generated schema globals stay intact.
func DeferUsageIndexes(next schema.Creator) schema.Creator {
	return schema.CreateFunc(func(ctx context.Context, tables ...*schema.Table) error {
		planned := append([]*schema.Table(nil), tables...)
		for i, table := range planned {
			if table.Name != "usage_logs" {
				continue
			}
			copy := *table
			copy.Indexes = nil
			for _, index := range table.Indexes {
				if index.Unique {
					copy.Indexes = append(copy.Indexes, index)
				}
			}
			planned[i] = &copy
		}
		return next.Create(ctx, planned...)
	})
}

func maintenanceOnly(upgrade systemUpgrade) bool {
	return optionalUsageDataUpgrade(upgrade) || upgrade.ID == "20260726100000_api_key_usage_lookup_index" || upgrade.ID == "20260907140000_usage_pagination_indexes" || strings.Contains(upgrade.SQL, "-- maintenance: true")
}

func optionalUsageDataUpgrade(upgrade systemUpgrade) bool {
	return upgrade.ID == "20260528075600_usage_logs_upgrade" || upgrade.ID == "20260726180000_monitor_schema_cleanup" || upgrade.ID == "20260731170000_account_request_times"
}

func maintenanceUpgradeSQL(upgrade systemUpgrade) string {
	if upgrade.ID != "20260731170000_account_request_times" {
		return upgrade.SQL
	}
	// Preserve the legacy probe/access cleanup without overwriting a timestamp
	// concurrently advanced by an online billing transaction.
	return `ALTER TABLE public.accounts ADD COLUMN IF NOT EXISTS last_probe_at timestamptz NULL;
WITH previous AS MATERIALIZED (SELECT id,last_used_at FROM public.accounts WHERE deleted_at IS NULL),
latest AS (SELECT account_usage_logs AS id,MAX(created_at) AS used_at FROM public.usage_logs WHERE account_usage_logs IS NOT NULL GROUP BY account_usage_logs)
UPDATE public.accounts AS account SET last_used_at=latest.used_at
FROM previous LEFT JOIN latest ON latest.id=previous.id
WHERE account.id=previous.id AND account.last_used_at IS NOT DISTINCT FROM previous.last_used_at;`
}

var concurrentIndexName = regexp.MustCompile(`(?is)^CREATE\s+INDEX\s+CONCURRENTLY\s+IF\s+NOT\s+EXISTS\s+([a-z_][a-z0-9_]*)\s`)

func stripLeadingSQLComments(statement string) string {
	statement = strings.TrimSpace(statement)
	for strings.HasPrefix(statement, "--") {
		_, rest, ok := strings.Cut(statement, "\n")
		if !ok {
			return ""
		}
		statement = strings.TrimSpace(rest)
	}
	return statement
}

func ensureUsageIndex(ctx context.Context, conn *sql.Conn, name, statement string) (bool, error) {
	lookup := func() (bool, error) {
		var valid bool
		err := conn.QueryRowContext(ctx, `SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname=$1`, name).Scan(&valid)
		return valid, err
	}
	valid, err := lookup()
	if err == nil && valid {
		return false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		if _, err = conn.ExecContext(ctx, "DROP INDEX CONCURRENTLY public."+pq.QuoteIdentifier(name)); err != nil {
			return false, err
		}
	}
	slog.Info("usage_index_build_start", "index", name)
	if _, err = conn.ExecContext(ctx, statement); err != nil {
		return false, err
	}
	valid, err = lookup()
	if err != nil {
		return false, err
	}
	if !valid {
		return false, fmt.Errorf("index %s remains invalid", name)
	}
	slog.Info("usage_index_build_done", "index", name)
	return true, nil
}

// RunUsageMaintenance owns a bounded connection and migration lock. It is
// never awaited before HTTP startup and repairs canceled concurrent builds.
func RunUsageMaintenance(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	var locked bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", systemUpgradeAdvisoryLockKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if _, err := conn.ExecContext(cleanup, "SELECT pg_advisory_unlock($1)", systemUpgradeAdvisoryLockKey); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err = conn.ExecContext(ctx, "SET lock_timeout='2s'"); err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if _, err := conn.ExecContext(cleanup, "RESET lock_timeout"); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	for _, table := range migrate.Tables {
		if table.Name != "usage_logs" {
			continue
		}
		for _, index := range table.Indexes {
			if index.Unique {
				continue
			}
			var columns []string
			for _, column := range index.Columns {
				columns = append(columns, pq.QuoteIdentifier(column.Name))
			}
			statement := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + pq.QuoteIdentifier(index.Name) + " ON public.usage_logs (" + strings.Join(columns, ",") + ")"
			if _, err := ensureUsageIndex(ctx, conn, index.Name, statement); err != nil {
				return err
			}
		}
	}
	for _, upgrade := range loadSystemUpgrades() {
		if !maintenanceOnly(upgrade) {
			continue
		}
		var checksum sql.NullString
		err := conn.QueryRowContext(ctx, "SELECT checksum FROM public.system_upgrade WHERE id=$1", upgrade.ID).Scan(&checksum)
		applied := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if applied && checksum.Valid && checksum.String != "" && checksum.String != upgrade.Checksum {
			return fmt.Errorf("maintenance migration %s checksum mismatch", upgrade.ID)
		}
		changed := false
		started := time.Now()
		for _, statement := range splitSQLStatements(maintenanceUpgradeSQL(upgrade)) {
			statement = stripLeadingSQLComments(statement)
			if match := concurrentIndexName.FindStringSubmatch(statement); match != nil {
				created, err := ensureUsageIndex(ctx, conn, match[1], statement)
				if err != nil {
					return err
				}
				changed = changed || created
			} else if strings.HasPrefix(strings.ToUpper(statement), "ANALYZE ") {
				if !applied || changed {
					if _, err := conn.ExecContext(ctx, statement); err != nil {
						return err
					}
				}
			} else if statement != "" && optionalUsageDataUpgrade(upgrade) {
				if !applied {
					if _, err := conn.ExecContext(ctx, statement); err != nil {
						return err
					}
				}
			} else if statement != "" {
				return fmt.Errorf("unsupported maintenance statement in %s", upgrade.ID)
			}
		}
		if !applied {
			if _, err := conn.ExecContext(ctx, `INSERT INTO public.system_upgrade(id,description,checksum,duration_ms) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, upgrade.ID, upgrade.Description, upgrade.Checksum, time.Since(started).Milliseconds()); err != nil {
				return err
			}
		}
	}
	return nil
}
