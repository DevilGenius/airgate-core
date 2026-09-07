// Package usageprojection maintains verified usage-reporting projections.
package usageprojection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DevilGenius/airgate-core/internal/safego"
)

const AdvisoryLockKey int64 = 20260908070000

var ErrVerificationBusy = errors.New("another usage projection verifier is running")

type metric struct {
	name       string
	expression string
	money      bool
}

type projection struct {
	name           string
	keys           []string
	keyExpressions []string
	actualKeys     []string
	actualFrom     string
	metrics        []metric
}

func projections() []projection {
	common := []metric{
		{"requests", "COUNT(*)", false},
		{"input_tokens", "SUM(input_tokens)", false},
		{"output_tokens", "SUM(output_tokens)", false},
		{"cached_input_tokens", "SUM(cached_input_tokens)", false},
		{"cache_creation_tokens", "SUM(cache_creation_tokens)", false},
		{"actual_cost", "SUM(actual_cost)", true},
		{"total_cost", "SUM(total_cost)", true},
	}
	hourly := projection{
		name:           "usage_hourly_rollups",
		keys:           []string{"bucket_start", "user_id", "model"},
		keyExpressions: []string{"date_trunc('hour',created_at,'UTC')", "COALESCE(NULLIF(user_id_snapshot,0),user_usage_logs,0)", "model"},
		actualKeys:     []string{"r.bucket_start", "r.user_id", "r.model"},
		actualFrom:     "usage_hourly_rollups r",
		metrics:        append([]metric(nil), common...),
	}
	image := "LOWER(TRIM(model)) LIKE 'gpt-image%'"
	hourly.metrics = append(hourly.metrics,
		metric{"image_requests", "SUM(CASE WHEN " + image + " THEN 1 ELSE 0 END)", false},
		metric{"non_image_requests", "SUM(CASE WHEN NOT (" + image + ") THEN 1 ELSE 0 END)", false},
		metric{"non_image_duration_ms", "SUM(CASE WHEN NOT (" + image + ") THEN duration_ms ELSE 0 END)", false},
		metric{"image_duration_ms", "SUM(CASE WHEN " + image + " THEN duration_ms ELSE 0 END)", false},
	)
	for _, name := range []string{"first_event", "first_token"} {
		condition := "NOT (" + image + ") AND " + name + "_ms>0"
		hourly.metrics = append(hourly.metrics,
			metric{name + "_requests", "SUM(CASE WHEN " + condition + " THEN 1 ELSE 0 END)", false},
			metric{name + "_ms", "SUM(CASE WHEN " + condition + " THEN " + name + "_ms ELSE 0 END)", false},
		)
	}
	api := projection{
		name:           "usage_api_key_hourly_rollups",
		keys:           []string{"bucket_start", "api_key_id", "user_id", "group_id", "account_id", "platform", "model"},
		keyExpressions: []string{"date_trunc('hour',created_at,'UTC')", "COALESCE(api_key_usage_logs,0)", "COALESCE(NULLIF(user_id_snapshot,0),user_usage_logs,0)", "COALESCE(group_usage_logs,0)", "COALESCE(account_usage_logs,0)", "platform", "model"},
		// Foreign-key deletion clears detail relations, while rollups intentionally
		// retain historical IDs. Compare both sides in the same effective namespace.
		actualKeys: []string{"r.bucket_start", "CASE WHEN k.id IS NULL THEN 0 ELSE r.api_key_id END", "r.user_id", "CASE WHEN g.id IS NULL THEN 0 ELSE r.group_id END", "CASE WHEN a.id IS NULL THEN 0 ELSE r.account_id END", "r.platform", "r.model"},
		actualFrom: "usage_api_key_hourly_rollups r LEFT JOIN api_keys k ON k.id=r.api_key_id LEFT JOIN groups g ON g.id=r.group_id LEFT JOIN accounts a ON a.id=r.account_id",
		metrics:    append(append([]metric(nil), common...), metric{"billed_cost", "SUM(billed_cost)", true}),
	}
	return []projection{hourly, api}
}

func (p projection) verificationSQL() string {
	var expected, actual, groups, checks []string
	for i, key := range p.keys {
		expected = append(expected, p.keyExpressions[i]+" AS "+key)
		actual = append(actual, p.actualKeys[i]+" AS "+key)
		groups = append(groups, fmt.Sprint(i+1))
	}
	checks = append(checks, "e.bucket_start IS NULL", "r.bucket_start IS NULL")
	for _, metric := range p.metrics {
		expected = append(expected, "COALESCE("+metric.expression+",0) AS "+metric.name)
		actual = append(actual, "COALESCE(SUM(r."+metric.name+"),0) AS "+metric.name)
		check := "e." + metric.name + " IS DISTINCT FROM r." + metric.name
		if metric.money {
			// Legacy numeric(20,8) UPSERTs rounded each contributing batch. This
			// conservative bound permits rounding, but never missing facts/tokens.
			check = "ABS(e." + metric.name + "-r." + metric.name + ")>GREATEST(0.0000001,e.requests*0.00000001)"
		}
		checks = append(checks, check)
	}
	return "WITH expected AS (SELECT " + strings.Join(expected, ",") + " FROM usage_logs GROUP BY " + strings.Join(groups, ",") + "), actual AS (SELECT " + strings.Join(actual, ",") + " FROM " + p.actualFrom + " GROUP BY " + strings.Join(groups, ",") + ") SELECT COUNT(*) FROM expected e FULL JOIN actual r USING (" + strings.Join(p.keys, ",") + ") WHERE " + strings.Join(checks, " OR ")
}

// VerifyPending checks existing data without rewriting aggregates or billing.
// Each projection is verified and marked in a single consistent snapshot. Live
// billing commits its detail and rollups atomically and is not locked out.
func VerifyPending(ctx context.Context, db *sql.DB) error {
	for _, p := range projections() {
		if err := verifyProjection(ctx, db, p); err != nil {
			return err
		}
	}
	return nil
}

func verifyProjection(ctx context.Context, db *sql.DB, p projection) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked bool
	if err := tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock($1)", AdvisoryLockKey).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return ErrVerificationBusy
	}
	var version int
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT version,state FROM usage_rollup_coverage WHERE projection=$1", p.name).Scan(&version, &state); err != nil {
		return err
	}
	if version != 1 || state == "ready" {
		return nil
	}
	started := time.Now()
	var mismatches int64
	if err := tx.QueryRowContext(ctx, p.verificationSQL()).Scan(&mismatches); err != nil {
		return err
	}
	state = "ready"
	if mismatches > 0 {
		state = "failed"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_rollup_coverage SET state=$2, verified_at=CASE WHEN $2='ready' THEN now() ELSE NULL END, updated_at=now() WHERE projection=$1 AND version=1`, p.name, state); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("usage_rollup_coverage_verified", "projection", p.name, "state", state, "mismatched_buckets", mismatches, "duration_ms", time.Since(started).Milliseconds())
	return nil
}

// StartVerification keeps one bounded maintenance scan out of HTTP startup and
// retries transient failures. Verified projections are a cheap no-op on restart.
func StartVerification(ctx context.Context, db *sql.DB) {
	safego.Go("usage-rollup-coverage", func() {
		for attempt := 0; attempt < 3; attempt++ {
			verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			err := VerifyPending(verifyCtx, db)
			cancel()
			if err == nil || ctx.Err() != nil {
				return
			}
			slog.Warn("usage_rollup_verification_failed", "attempt", attempt+1, "error", err)
			if attempt == 2 {
				return
			}
			timer := time.NewTimer(time.Duration(attempt+1) * 30 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})
}
