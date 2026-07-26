-- description: Optimize API key usage summaries on large usage_logs tables.

CREATE INDEX CONCURRENTLY IF NOT EXISTS usage_log_api_key_created_at_cost
	ON public.usage_logs (api_key_usage_logs, created_at)
	INCLUDE (billed_cost, actual_cost)
	WHERE api_key_usage_logs IS NOT NULL;
