package bootstrap

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Lock contention postpones readiness, not process lifetime. The caller supplies
// a bounded statement or a transaction that rolls back before another attempt.
func retrySystemUpgradeLock(ctx context.Context, action string, run func(context.Context) error) error {
	delay := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := run(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var pgErr *pq.Error
		if !errors.As(err, &pgErr) || (pgErr.Code != "55P03" && pgErr.Code != "40P01") {
			return err
		}
		slog.Warn("system_upgrade_lock_retry", "action", action, "attempt", attempt, "sqlstate", pgErr.Code, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, 10*time.Second)
	}
}

func execSystemUpgrade(ctx context.Context, conn *sql.Conn, action, statement string, args ...any) error {
	return retrySystemUpgradeLock(ctx, action, func(attemptCtx context.Context) error {
		return execSystemUpgradeOnce(attemptCtx, conn, statement, args...)
	})
}

func execSystemUpgradeOnce(ctx context.Context, conn *sql.Conn, statement string, args ...any) error {
	statementCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := conn.ExecContext(statementCtx, statement, args...)
	return err
}

// Preserve explicit transaction boundaries in embedded migrations. A failed
// transaction cannot retry an individual statement until it has rolled back.
func systemUpgradeSQLBatches(source string) ([][]string, error) {
	statements := splitSQLStatements(source)
	var batches [][]string
	start := -1
	for i, statement := range statements {
		command := strings.ToUpper(strings.Join(strings.Fields(stripLeadingSQLComments(statement)), " "))
		switch command {
		case "BEGIN", "BEGIN WORK", "BEGIN TRANSACTION", "START TRANSACTION":
			if start >= 0 {
				return nil, fmt.Errorf("nested migration transaction")
			}
			start = i
		case "COMMIT", "COMMIT WORK", "COMMIT TRANSACTION", "END":
			if start < 0 {
				return nil, fmt.Errorf("migration COMMIT without BEGIN")
			}
			batches = append(batches, statements[start:i+1])
			start = -1
		case "ROLLBACK", "ROLLBACK WORK", "ROLLBACK TRANSACTION":
			return nil, fmt.Errorf("migration must commit its transaction")
		default:
			if start < 0 {
				batches = append(batches, statements[i:i+1])
			}
		}
	}
	if start >= 0 {
		return nil, fmt.Errorf("unclosed migration transaction")
	}
	return batches, nil
}

func execSystemUpgradeBatch(ctx context.Context, conn *sql.Conn, action string, statements []string) error {
	if len(statements) == 1 {
		return execSystemUpgrade(ctx, conn, action, statements[0])
	}
	return retrySystemUpgradeLock(ctx, action, func(attemptCtx context.Context) error {
		for _, statement := range statements {
			if err := execSystemUpgradeOnce(attemptCtx, conn, statement); err != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, rollbackErr := conn.ExecContext(cleanup, "ROLLBACK")
				cancel()
				if rollbackErr != nil {
					_ = conn.Raw(func(any) error { return driver.ErrBadConn })
					return fmt.Errorf("migration rollback failed: %v (original error: %v)", rollbackErr, err)
				}
				return err
			}
		}
		return nil
	})
}
