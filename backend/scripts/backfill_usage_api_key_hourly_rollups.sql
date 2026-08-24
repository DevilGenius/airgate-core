-- Manual post-startup backfill for usage_api_key_hourly_rollups.
-- This file is outside internal/bootstrap/migrations and is never executed
-- automatically by the service. Run it manually after the service is healthy.

BEGIN;

TRUNCATE TABLE public.usage_api_key_hourly_rollups;

INSERT INTO public.usage_api_key_hourly_rollups (
	bucket_start, api_key_id, user_id, group_id, account_id, platform, model,
	requests, input_tokens, output_tokens, cached_input_tokens, cache_creation_tokens,
	total_cost, actual_cost, billed_cost, updated_at
)
SELECT
	date_trunc('hour', created_at),
	COALESCE(api_key_usage_logs, 0),
	COALESCE(NULLIF(user_id_snapshot, 0), user_usage_logs, 0),
	COALESCE(group_usage_logs, 0),
	COALESCE(account_usage_logs, 0),
	platform,
	model,
	COUNT(*)::bigint,
	COALESCE(SUM(input_tokens), 0)::bigint,
	COALESCE(SUM(output_tokens), 0)::bigint,
	COALESCE(SUM(cached_input_tokens), 0)::bigint,
	COALESCE(SUM(cache_creation_tokens), 0)::bigint,
	COALESCE(SUM(total_cost), 0),
	COALESCE(SUM(actual_cost), 0),
	COALESCE(SUM(billed_cost), 0),
	now()
FROM public.usage_logs
GROUP BY 1, 2, 3, 4, 5, 6, 7
ON CONFLICT DO NOTHING;

ANALYZE public.usage_api_key_hourly_rollups;

COMMIT;
