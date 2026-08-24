-- description: Add API-key hourly usage rollups for admin summary cards.

CREATE TABLE IF NOT EXISTS public.usage_api_key_hourly_rollups (
	bucket_start timestamptz NOT NULL,
	api_key_id integer NOT NULL DEFAULT 0,
	user_id integer NOT NULL DEFAULT 0,
	group_id integer NOT NULL DEFAULT 0,
	account_id integer NOT NULL DEFAULT 0,
	platform text NOT NULL DEFAULT '',
	model text NOT NULL DEFAULT '',
	requests bigint NOT NULL DEFAULT 0,
	input_tokens bigint NOT NULL DEFAULT 0,
	output_tokens bigint NOT NULL DEFAULT 0,
	cached_input_tokens bigint NOT NULL DEFAULT 0,
	cache_creation_tokens bigint NOT NULL DEFAULT 0,
	total_cost numeric(20,8) NOT NULL DEFAULT 0,
	actual_cost numeric(20,8) NOT NULL DEFAULT 0,
	billed_cost numeric(20,8) NOT NULL DEFAULT 0,
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (bucket_start, api_key_id, user_id, group_id, account_id, platform, model)
);

CREATE INDEX IF NOT EXISTS usage_api_key_hourly_rollups_bucket
	ON public.usage_api_key_hourly_rollups (bucket_start);
CREATE INDEX IF NOT EXISTS usage_api_key_hourly_rollups_key_bucket
	ON public.usage_api_key_hourly_rollups (api_key_id, bucket_start);
CREATE INDEX IF NOT EXISTS usage_api_key_hourly_rollups_user_bucket
	ON public.usage_api_key_hourly_rollups (user_id, bucket_start);
CREATE INDEX IF NOT EXISTS usage_api_key_hourly_rollups_account_bucket
	ON public.usage_api_key_hourly_rollups (account_id, bucket_start);
CREATE INDEX IF NOT EXISTS usage_api_key_hourly_rollups_platform_bucket
	ON public.usage_api_key_hourly_rollups (platform, bucket_start);
CREATE INDEX IF NOT EXISTS usage_api_key_hourly_rollups_model_bucket
	ON public.usage_api_key_hourly_rollups (model, bucket_start);
