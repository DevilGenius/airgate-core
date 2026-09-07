-- description: Add covering indexes and planner statistics for usage pagination.

CREATE INDEX CONCURRENTLY IF NOT EXISTS usage_log_owner_page
    ON public.usage_logs ((COALESCE(NULLIF(user_id_snapshot, 0), user_usage_logs, 0)), id DESC)
    INCLUDE (user_id_snapshot, user_usage_logs);

CREATE INDEX CONCURRENTLY IF NOT EXISTS usage_log_key_page
    ON public.usage_logs (api_key_usage_logs, id DESC)
    INCLUDE (user_id_snapshot, user_usage_logs)
    WHERE api_key_usage_logs IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS usage_log_model_page
    ON public.usage_logs (model, id DESC)
    INCLUDE (user_id_snapshot, user_usage_logs, api_key_usage_logs);

-- Populate planner statistics, including the new owner expression, in production
-- after all indexes exist. This runs as part of the upgrade, not on page requests.
ANALYZE public.usage_logs;
