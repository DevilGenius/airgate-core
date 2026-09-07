-- The in-place TRUNCATE/full-scan backfill has been retired.
-- Run from backend/, using the same DB owner as the core service:
--   go run ./cmd/usage-rollup-backfill -config ../.dev/config.yaml
-- Use -rebuild to replace already verified projections. The command resumes
-- an interrupted job, verifies shadow projections, and retains old table backups.
DO $guard$
BEGIN
    RAISE EXCEPTION 'Use cmd/usage-rollup-backfill; in-place online backfills are disabled';
END
$guard$;
