package usageprojection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lib/pq"

	"github.com/DevilGenius/airgate-core/internal/safego"
)

type maintenanceState struct {
	active  bool
	pending bool
	failed  bool
}

// Inspect only small control tables. A completed installation must not scan its
// history, create shadow tables, or start another rebuild on every restart.
func inspectMaintenance(ctx context.Context, db *sql.DB, schema string) (maintenanceState, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var state maintenanceState
	prefix := pq.QuoteIdentifier(schema) + "."
	rows, err := db.QueryContext(ctx, "SELECT version,state FROM "+prefix+"usage_rollup_coverage WHERE projection IN ('usage_hourly_rollups','usage_api_key_hourly_rollups')")
	if err != nil {
		return state, err
	}
	count := 0
	for rows.Next() {
		var version int
		var status string
		if err := rows.Scan(&version, &status); err != nil {
			_ = rows.Close()
			return state, err
		}
		if version != 1 {
			_ = rows.Close()
			return state, fmt.Errorf("unsupported usage projection version %d", version)
		}
		count++
		switch status {
		case "ready":
		case "pending":
			state.pending = true
		case "failed":
			state.failed = true
		default:
			_ = rows.Close()
			return state, fmt.Errorf("unsupported usage projection state %q", status)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return state, err
	}
	if count != 2 {
		return state, fmt.Errorf("usage projection coverage is not initialized")
	}
	var hasJobs bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", prefix+"usage_rollup_rebuilds").Scan(&hasJobs); err != nil {
		return state, err
	}
	if hasJobs {
		if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+prefix+"usage_rollup_rebuilds WHERE state<>'ready')").Scan(&state.active); err != nil {
			return state, err
		}
	}
	return state, nil
}

// reconcileMaintenance is one bounded pass. Existing jobs take priority even
// when the serving projection is ready: an interrupted forced rebuild may still
// own a capture trigger and must be finished, not silently abandoned.
func reconcileMaintenance(ctx context.Context, db *sql.DB, options RebuildOptions) error {
	if options.Schema == "" {
		options.Schema = "public"
	}
	if !schemaPattern.MatchString(options.Schema) {
		return fmt.Errorf("invalid projection schema")
	}
	options.Force = false
	state, err := inspectMaintenance(ctx, db, options.Schema)
	if err != nil {
		return err
	}
	if state.active {
		return Rebuild(ctx, db, options)
	}
	if !state.pending && !state.failed {
		return nil
	}
	if state.pending && !state.failed {
		checkCtx, stopCheck := context.WithTimeout(ctx, 10*time.Second)
		prefix := pq.QuoteIdentifier(options.Schema) + "."
		var emptyProjection bool
		err = db.QueryRowContext(checkCtx, "SELECT EXISTS(SELECT 1 FROM "+prefix+"usage_logs) AND (NOT EXISTS(SELECT 1 FROM "+prefix+"usage_hourly_rollups) OR NOT EXISTS(SELECT 1 FROM "+prefix+"usage_api_key_hourly_rollups))").Scan(&emptyProjection)
		stopCheck()
		if err != nil {
			return err
		}
		if emptyProjection {
			return Rebuild(ctx, db, options)
		}
		verifyTimeout := options.VerifyTimeout
		if verifyTimeout <= 0 {
			verifyTimeout = 30 * time.Minute
		}
		verifyCtx, cancel := context.WithTimeout(ctx, verifyTimeout)
		err = verifyPendingInSchema(verifyCtx, db, options.Schema)
		timedOut := errors.Is(verifyCtx.Err(), context.DeadlineExceeded)
		cancel()
		if err != nil {
			// An expensive initial verification must not prevent this
			// installation from reaching its resumable backfill path.
			if (timedOut || errors.Is(err, context.DeadlineExceeded)) && ctx.Err() == nil {
				return Rebuild(ctx, db, options)
			}
			return err
		}
		state, err = inspectMaintenance(ctx, db, options.Schema)
		if err != nil {
			return err
		}
		if !state.active && !state.pending && !state.failed {
			return nil
		}
	}
	slog.Info("usage_rollup_automatic_rebuild_start")
	return Rebuild(ctx, db, options)
}

type maintenancePolicy struct {
	attemptTimeout time.Duration
	retryDelay     time.Duration
	maxRetryDelay  time.Duration
}

func maintainUntilReady(ctx context.Context, run func(context.Context) error, policy maintenancePolicy) error {
	delay := policy.retryDelay
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, policy.attemptTimeout)
		err := run(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("usage_rollup_maintenance_retry", "error", err, "retry_after", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, policy.maxRetryDelay)
	}
}

// StartMaintenance is part of the server binary, including Docker images. It
// resumes database checkpoints and retries with a capped delay until ready or
// shutdown; each pass, SQL batch and lock wait keeps its own deadline.
func StartMaintenance(ctx context.Context, db *sql.DB) {
	safego.Go("usage-rollup-maintenance", func() {
		var lastReport time.Time
		var lastState string
		options := RebuildOptions{BatchSize: 5000, VerifyTimeout: 30 * time.Minute, Progress: func(progress Progress) {
			if progress.State == lastState && time.Since(lastReport) < 15*time.Second {
				return
			}
			lastReport = time.Now()
			lastState = progress.State
			slog.Info("usage_rollup_maintenance_progress", "job_id", progress.JobID, "state", progress.State, "cursor", progress.Cursor, "source_max", progress.SourceMax)
		}}
		err := maintainUntilReady(ctx, func(attemptCtx context.Context) error { return reconcileMaintenance(attemptCtx, db, options) }, maintenancePolicy{attemptTimeout: time.Hour, retryDelay: 15 * time.Second, maxRetryDelay: 5 * time.Minute})
		if err == nil {
			slog.Info("usage_rollup_maintenance_ready")
		}
	})
}
