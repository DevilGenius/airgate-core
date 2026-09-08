package usageprojection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Progress struct {
	JobID     string
	Cursor    int64
	SourceMax int64
	State     string
}

type RebuildOptions struct {
	Schema        string
	BatchSize     int
	VerifyTimeout time.Duration
	Force         bool
	Progress      func(Progress)
}

const rebuildTableSQL = `CREATE TABLE IF NOT EXISTS usage_rollup_rebuilds (
 id text PRIMARY KEY,
 state text NOT NULL CHECK (state IN ('backfilling','verified','ready')),
 source_max_id bigint NOT NULL DEFAULT 0,
 last_id bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
)`

var schemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Rebuild uses a persistent ID ledger and an INSERT transition-table trigger to
// merge backfill and live facts exactly once. No long transaction locks the
// serving projections, and an interrupted command resumes its committed cursor.
func Rebuild(ctx context.Context, db *sql.DB, options RebuildOptions) error {
	if options.Schema == "" {
		options.Schema = "public"
	}
	if !schemaPattern.MatchString(options.Schema) {
		return errors.New("invalid projection schema")
	}
	if options.BatchSize == 0 {
		options.BatchSize = 2000
	}
	if options.BatchSize < 1 || options.BatchSize > 10000 {
		return errors.New("batch size must be between 1 and 10000")
	}
	if options.VerifyTimeout == 0 {
		options.VerifyTimeout = 5 * time.Minute
	}
	if options.VerifyTimeout <= 0 || options.VerifyTimeout > 30*time.Minute {
		return errors.New("verification timeout must be between zero and 30 minutes")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err = conn.ExecContext(ctx, "SET search_path TO "+pq.QuoteIdentifier(options.Schema)+", pg_catalog"); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanup, "RESET search_path; RESET lock_timeout"); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err = conn.ExecContext(ctx, "SET lock_timeout='1s'"); err != nil {
		return err
	}
	var locked bool
	releaseLock := true
	defer func() {
		if !releaseLock {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanup, "SELECT pg_advisory_unlock($1)", AdvisoryLockKey); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", AdvisoryLockKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		releaseLock = false
		return ErrVerificationBusy
	}
	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = conn.ExecContext(setupCtx, rebuildTableSQL)
	if err == nil {
		_, err = conn.ExecContext(setupCtx, `CREATE UNIQUE INDEX IF NOT EXISTS usage_rollup_rebuilds_active ON usage_rollup_rebuilds ((true)) WHERE state<>'ready'`)
	}
	cancel()
	if err != nil {
		return err
	}
	var supported int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_rollup_coverage WHERE version=1 AND projection IN ('usage_hourly_rollups','usage_api_key_hourly_rollups')`).Scan(&supported); err != nil {
		return err
	}
	if supported != 2 {
		return errors.New("projection schema version is unsupported; use the matching core maintenance command")
	}
	job, err := loadRebuild(ctx, conn)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		var ready int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_rollup_coverage WHERE version=1 AND state='ready' AND projection IN ('usage_hourly_rollups','usage_api_key_hourly_rollups')`).Scan(&ready); err != nil {
			return err
		}
		if ready == 2 && !options.Force {
			return nil
		}
		job, err = prepareRebuild(ctx, conn, options.Schema)
		if err != nil {
			return err
		}
	}
	report := func() {
		if options.Progress != nil {
			options.Progress(job)
		}
	}
	report()
	for job.State == "backfilling" {
		if err := ctx.Err(); err != nil {
			return err
		}
		var count int
		job.Cursor, count, err = rebuildBatch(ctx, conn, job, options.BatchSize)
		if err != nil {
			return err
		}
		report()
		if count == 0 {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if job.State != "verified" {
		if err := verifyRebuild(ctx, conn, job, options.VerifyTimeout); err != nil {
			return err
		}
		job.State = "verified"
		report()
	}
	if err := cutoverRebuild(ctx, conn, job); err != nil {
		return err
	}
	job.State = "ready"
	report()
	return nil
}

func loadRebuild(ctx context.Context, conn *sql.Conn) (Progress, error) {
	var job Progress
	err := conn.QueryRowContext(ctx, `SELECT id,last_id,source_max_id,state FROM usage_rollup_rebuilds WHERE state<>'ready' LIMIT 1`).Scan(&job.JobID, &job.Cursor, &job.SourceMax, &job.State)
	if err == nil {
		if len(job.JobID) != 12 || strings.Trim(job.JobID, "0123456789abcdef") != "" {
			return job, errors.New("invalid persisted rebuild ID")
		}
	}
	return job, err
}

func maintenanceTx(ctx context.Context, conn *sql.Conn) (*sql.Tx, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL lock_timeout='1s'`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func prepareRebuild(ctx context.Context, conn *sql.Conn, schema string) (Progress, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	job := Progress{JobID: strings.ReplaceAll(uuid.NewString(), "-", "")[:12], State: "backfilling"}
	tx, err := maintenanceTx(ctx, conn)
	if err != nil {
		return job, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range projections() {
		if _, err = tx.ExecContext(ctx, "CREATE TABLE "+pq.QuoteIdentifier(shadowName(p, job.JobID))+" (LIKE "+pq.QuoteIdentifier(p.name)+" INCLUDING ALL)"); err != nil {
			return job, err
		}
	}
	if _, err = tx.ExecContext(ctx, "CREATE TABLE "+pq.QuoteIdentifier(ledgerName(job.JobID))+" (usage_id bigint PRIMARY KEY)"); err != nil {
		return job, err
	}
	if _, err = tx.ExecContext(ctx, captureFunctionSQL(job.JobID, schema)); err != nil {
		return job, err
	}
	// The short lock installs capture after existing insert transactions finish.
	// A preallocated, late-committing ID is still captured by the new trigger.
	if _, err = tx.ExecContext(ctx, "LOCK TABLE usage_logs IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return job, err
	}
	if _, err = tx.ExecContext(ctx, "CREATE TRIGGER "+pq.QuoteIdentifier(captureName(job.JobID))+" AFTER INSERT ON usage_logs REFERENCING NEW TABLE AS inserted_usage_rows FOR EACH STATEMENT EXECUTE FUNCTION "+pq.QuoteIdentifier(captureName(job.JobID))+"()"); err != nil {
		return job, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM usage_logs").Scan(&job.SourceMax); err != nil {
		return job, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO usage_rollup_rebuilds(id,state,source_max_id) VALUES($1,$2,$3)", job.JobID, job.State, job.SourceMax); err != nil {
		return job, err
	}
	return job, tx.Commit()
}

func rebuildBatch(ctx context.Context, conn *sql.Conn, job Progress, size int) (int64, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	source := "SELECT " + sourceColumns + " FROM usage_logs WHERE id>$1 AND id<=$2 ORDER BY id LIMIT $3"
	// One statement is one atomic transaction. This avoids five network round
	// trips per batch while keeping facts, both projections and cursor together.
	checkpoint := `checkpoint AS (UPDATE usage_rollup_rebuilds SET last_id=COALESCE((SELECT MAX(id) FROM picked),$1),updated_at=now() WHERE id=$4 AND state='backfilling' AND last_id=$1 RETURNING last_id)`
	query := applyBatchSQL(job.JobID, source, "SELECT last_id,(SELECT COUNT(*) FROM picked) FROM checkpoint", checkpoint)
	var cursor int64
	var count int
	if err := conn.QueryRowContext(ctx, query, job.Cursor, job.SourceMax, size, job.JobID).Scan(&cursor, &count); err != nil {
		return job.Cursor, 0, err
	}
	return cursor, count, nil
}

func verifyRebuild(ctx context.Context, conn *sql.Conn, job Progress, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for _, p := range projections() {
		if _, err := conn.ExecContext(ctx, "ANALYZE "+pq.QuoteIdentifier(shadowName(p, job.JobID))); err != nil {
			return err
		}
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range projections() {
		p.actualFrom = strings.Replace(p.actualFrom, p.name, pq.QuoteIdentifier(shadowName(p, job.JobID)), 1)
		var mismatches int64
		if err := tx.QueryRowContext(ctx, p.verificationSQL()).Scan(&mismatches); err != nil {
			return err
		}
		if mismatches != 0 {
			return fmt.Errorf("%s rebuild has %d mismatched buckets; serving projection unchanged", p.name, mismatches)
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE usage_rollup_rebuilds SET state='verified',updated_at=now() WHERE id=$1", job.JobID); err != nil {
		return err
	}
	return tx.Commit()
}

func cutoverRebuild(ctx context.Context, conn *sql.Conn, job Progress) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := maintenanceTx(ctx, conn)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "LOCK TABLE usage_logs IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return err
	}
	var supported int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_rollup_coverage WHERE version=1 AND projection IN ('usage_hourly_rollups','usage_api_key_hourly_rollups')`).Scan(&supported); err != nil {
		return err
	}
	if supported != 2 {
		return errors.New("projection version changed during rebuild; serving tables unchanged")
	}
	for _, p := range projections() {
		if _, err = tx.ExecContext(ctx, "LOCK TABLE "+pq.QuoteIdentifier(p.name)+","+pq.QuoteIdentifier(shadowName(p, job.JobID))+" IN ACCESS EXCLUSIVE MODE"); err != nil {
			return err
		}
		var shapeChanged bool
		if err = tx.QueryRowContext(ctx, `WITH old_shape AS (SELECT attname,atttypid,atttypmod,attnotnull FROM pg_attribute WHERE attrelid=$1::regclass AND attnum>0 AND NOT attisdropped), new_shape AS (SELECT attname,atttypid,atttypmod,attnotnull FROM pg_attribute WHERE attrelid=$2::regclass AND attnum>0 AND NOT attisdropped) SELECT EXISTS((SELECT * FROM old_shape EXCEPT SELECT * FROM new_shape) UNION ALL (SELECT * FROM new_shape EXCEPT SELECT * FROM old_shape))`, p.name, shadowName(p, job.JobID)).Scan(&shapeChanged); err != nil {
			return err
		}
		if shapeChanged {
			return fmt.Errorf("%s schema changed during rebuild; serving tables unchanged", p.name)
		}
		// Views depend on table OIDs, not their names; never silently strand one.
		var dependencies int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_depend d JOIN pg_rewrite r ON r.oid=d.objid WHERE d.classid='pg_rewrite'::regclass AND d.refobjid=$1::regclass`, p.name).Scan(&dependencies); err != nil {
			return err
		}
		if dependencies != 0 {
			return fmt.Errorf("%s has dependent views; migrate those before cutover", p.name)
		}
		if _, err = tx.ExecContext(ctx, "ALTER TABLE "+pq.QuoteIdentifier(p.name)+" RENAME TO "+pq.QuoteIdentifier(backupName(p, job.JobID))); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "ALTER TABLE "+pq.QuoteIdentifier(shadowName(p, job.JobID))+" RENAME TO "+pq.QuoteIdentifier(p.name)); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DROP TRIGGER "+pq.QuoteIdentifier(captureName(job.JobID))+" ON usage_logs"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DROP FUNCTION "+pq.QuoteIdentifier(captureName(job.JobID))+"()"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE usage_rollup_coverage SET state='ready',covered_from=NULL,verified_at=now(),updated_at=now() WHERE version=1 AND projection IN ('usage_hourly_rollups','usage_api_key_hourly_rollups')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE usage_rollup_rebuilds SET state='ready',updated_at=now() WHERE id=$1 AND state='verified'", job.JobID); err != nil {
		return err
	}
	return tx.Commit()
}
