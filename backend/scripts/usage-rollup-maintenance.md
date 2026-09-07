# Usage rollup maintenance

The server performs this maintenance automatically after HTTP startup, including
Docker deployments. No Go toolchain, interactive shell or manual backfill command
is required in the production container. Existing verified projections are a
cheap no-op. An unfinished job is resumed before checking whether the serving
tables are already ready; missing or incomplete history is rebuilt automatically.

Each automatic pass has a one-hour deadline and persists its progress in
PostgreSQL. Database errors, leadership contention, lock timeouts and unfinished
passes retry with backoff from fifteen seconds up to five minutes. Shutdown
cancels the active pass; the next container/server start resumes its checkpoint.
Automatic historical batches use 5000 rows, and verification has a thirty-minute
deadline. Progress is emitted to the ordinary server/container logs as
`usage_rollup_maintenance_progress` with job ID, state, cursor and source maximum.
An empty projection with existing history goes straight to rebuild. If initial
verification times out, maintenance advances to resumable rebuild instead of
repeating that initial scan forever. Final verification still must succeed before
the serving tables can be replaced.

The command below remains an optional administrator/debugging tool. Run it as
the core database owner from `backend/`, after applying the current schema:

```powershell
go run ./cmd/usage-rollup-backfill -config ../.dev/config.yaml
```

To explicitly rebuild verified projections with the optional command, add
`-rebuild`. Interrupting or reaching `-timeout` leaves a resumable job. Run the
same command again, or restart the server, to continue. The command's `-batch-size`
defaults to 2000 rows and is capped at 10000;
individual data transactions have a ten-second deadline and a one-second lock
wait. `-verify-timeout` defaults to five minutes and may be increased to thirty
minutes for a large historical dataset.

The command requires the same database role used by core writers. One advisory
lock coordinates maintenance and coverage verification across instances. A
statement-level insert trigger projects new usage into shadow tables while an
ID ledger deduplicates historical batches, overlaps, and late-committing IDs.
The serving tables stay available while backfill and consistent-snapshot
verification run. Billing facts and account balances are never rewritten.

Cutover acquires short locks, removes capture, renames both projections, and
marks coverage ready in one transaction. If an active reader prevents cutover,
the command fails within its lock budget; capture and progress remain intact,
and the next run retries cutover. Dependent SQL views are detected and must be
migrated explicitly before a table swap.

Old tables named `<projection>_old_<job ID>`, the ID ledger, and the job record
remain for audit. Preserve their storage until the replacement is accepted.
Do not run independent usage-log archival or rewrite historical billing facts
during a rebuild. Foreign-key deletion is normalized during verification.

The old `backfill_usage_api_key_hourly_rollups.sql` now fails without changing
data. Its in-place `TRUNCATE` workflow must not be used while serving requests.
Published migration files and checksums are retained; the startup executor
applies only the schema portion of the legacy hourly-rollup migration. Historical
data is maintained by the automatic background workflow instead of blocking HTTP
startup. Existing serving tables remain queryable during a rebuild; genuinely
unverified historical reports remain unavailable until coverage is established.

Usage-log secondary indexes and the versioned concurrent-index migrations also
run after HTTP starts. Required uniqueness constraints remain in startup schema
validation. The background worker checks index validity, repairs interrupted
concurrent builds, and only records a deferred migration after it succeeds.
Startup migration connection/lock/statement waits are bounded; a failed cleanup
discards its connection instead of returning a session lock to the pool.
