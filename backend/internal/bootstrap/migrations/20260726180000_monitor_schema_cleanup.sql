-- description: Drop unused monitor indexes and the retired monitor notification columns.

-- monitor_request_events is the highest-volume monitor table, so every index is
-- paid on each insert. pg_stat_user_indexes recorded 0-4 scans for these while
-- created_at recorded ~200k over the same window.
DROP INDEX CONCURRENTLY IF EXISTS public.monitorrequestevent_error_code_created_at;

DROP INDEX CONCURRENTLY IF EXISTS public.monitorrequestevent_endpoint_created_at;

DROP INDEX CONCURRENTLY IF EXISTS public.monitorrequestevent_account_id_created_at;

DROP INDEX CONCURRENTLY IF EXISTS public.monitorrequestevent_group_id_created_at;

-- Rows here are append-only and never coalesced by hash; traces are resolved
-- from monitor_request_trace by its own unique hash, not from this side.
DROP INDEX CONCURRENTLY IF EXISTS public.monitorrequestevent_hash;

DROP INDEX CONCURRENTLY IF EXISTS public.monitorrequestevent_trace_hash;

-- Filtering monitor events by type alone is unused; status/severity carry the
-- listing load and keep their indexes.
DROP INDEX CONCURRENTLY IF EXISTS public.monitorevent_type_updated_at;

DROP INDEX CONCURRENTLY IF EXISTS public.monitorevent_status_type_updated_at;

-- Retention sweeps use expires_at; last_seen_at is written but never queried.
DROP INDEX CONCURRENTLY IF EXISTS public.monitorrequesttrace_last_seen_at;

-- The monitor notification loop was removed, so delivery state no longer belongs
-- on the business event. The narrow (status, severity) index below replaces the
-- one being dropped here: the summary aggregates over the whole table and was
-- relying on this index only because it was the smallest one covering both
-- columns, not because of next_notify_at.
CREATE INDEX CONCURRENTLY IF NOT EXISTS monitorevent_status_severity
	ON public.monitor_events (status, severity);

DROP INDEX CONCURRENTLY IF EXISTS public.monitorevent_status_severity_next_notify_at;

ALTER TABLE public.monitor_events DROP COLUMN IF EXISTS last_notified_at;

ALTER TABLE public.monitor_events DROP COLUMN IF EXISTS next_notify_at;

ALTER TABLE public.monitor_events DROP COLUMN IF EXISTS notify_error;
