-- description: Add hourly usage rollups for dashboard queries.

CREATE TABLE IF NOT EXISTS public.usage_hourly_rollups (
	bucket_start timestamptz NOT NULL,
	user_id integer NOT NULL DEFAULT 0,
	user_email text NOT NULL DEFAULT '',
	model text NOT NULL,
	requests bigint NOT NULL DEFAULT 0,
	input_tokens bigint NOT NULL DEFAULT 0,
	output_tokens bigint NOT NULL DEFAULT 0,
	cached_input_tokens bigint NOT NULL DEFAULT 0,
	cache_creation_tokens bigint NOT NULL DEFAULT 0,
	actual_cost numeric(20,8) NOT NULL DEFAULT 0,
	total_cost numeric(20,8) NOT NULL DEFAULT 0,
	image_requests bigint NOT NULL DEFAULT 0,
	non_image_requests bigint NOT NULL DEFAULT 0,
	non_image_duration_ms bigint NOT NULL DEFAULT 0,
	first_token_requests bigint NOT NULL DEFAULT 0,
	first_token_ms bigint NOT NULL DEFAULT 0,
	image_duration_ms bigint NOT NULL DEFAULT 0,
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (bucket_start, user_id, model)
);

CREATE INDEX IF NOT EXISTS usage_hourly_rollups_user_bucket
	ON public.usage_hourly_rollups (user_id, bucket_start);

CREATE INDEX IF NOT EXISTS usage_hourly_rollups_model_bucket
	ON public.usage_hourly_rollups (model, bucket_start);

TRUNCATE TABLE public.usage_hourly_rollups;

INSERT INTO public.usage_hourly_rollups (
	bucket_start,
	user_id,
	user_email,
	model,
	requests,
	input_tokens,
	output_tokens,
	cached_input_tokens,
	cache_creation_tokens,
	actual_cost,
	total_cost,
	image_requests,
	non_image_requests,
	non_image_duration_ms,
	first_token_requests,
	first_token_ms,
	image_duration_ms,
	updated_at
)
SELECT
	date_trunc('hour', created_at) AS bucket_start,
	COALESCE(NULLIF(user_id_snapshot, 0), user_usage_logs, 0) AS user_id,
	COALESCE(MAX(NULLIF(user_email_snapshot, '')), '') AS user_email,
	model,
	COUNT(*)::bigint AS requests,
	COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
	COALESCE(SUM(output_tokens), 0)::bigint AS output_tokens,
	COALESCE(SUM(cached_input_tokens), 0)::bigint AS cached_input_tokens,
	COALESCE(SUM(cache_creation_tokens), 0)::bigint AS cache_creation_tokens,
	COALESCE(SUM(actual_cost), 0) AS actual_cost,
	COALESCE(SUM(total_cost), 0) AS total_cost,
	COALESCE(SUM(CASE WHEN LOWER(TRIM(model)) LIKE 'gpt-image%' THEN 1 ELSE 0 END), 0)::bigint AS image_requests,
	COALESCE(SUM(CASE WHEN NOT (LOWER(TRIM(model)) LIKE 'gpt-image%') THEN 1 ELSE 0 END), 0)::bigint AS non_image_requests,
	COALESCE(SUM(CASE WHEN NOT (LOWER(TRIM(model)) LIKE 'gpt-image%') THEN duration_ms ELSE 0 END), 0)::bigint AS non_image_duration_ms,
	COALESCE(SUM(CASE WHEN NOT (LOWER(TRIM(model)) LIKE 'gpt-image%') AND first_token_ms > 0 THEN 1 ELSE 0 END), 0)::bigint AS first_token_requests,
	COALESCE(SUM(CASE WHEN NOT (LOWER(TRIM(model)) LIKE 'gpt-image%') AND first_token_ms > 0 THEN first_token_ms ELSE 0 END), 0)::bigint AS first_token_ms,
	COALESCE(SUM(CASE WHEN LOWER(TRIM(model)) LIKE 'gpt-image%' THEN duration_ms ELSE 0 END), 0)::bigint AS image_duration_ms,
	now() AS updated_at
FROM public.usage_logs
GROUP BY 1, 2, model;

ANALYZE public.usage_hourly_rollups;
