-- description: Split usage first-event latency, TTFT, and WebSocket dial timing.

-- Ent 自动迁移先于版本 SQL 执行，会先补出一个全为 0 的 first_event_ms。
-- 删除该临时列后，将旧 first_token_ms 原地改名为 first_event_ms；RENAME/DROP
-- 都是 PostgreSQL 目录级操作，不逐行重写几百万条历史数据。
DO $$
BEGIN
	ALTER TABLE public.usage_logs
		DROP COLUMN IF EXISTS first_event_ms;

	ALTER TABLE public.usage_logs
		RENAME COLUMN first_token_ms TO first_event_ms;

	ALTER TABLE public.usage_logs
		ADD COLUMN first_token_ms bigint NOT NULL DEFAULT 0;

	ALTER TABLE public.usage_logs
		ADD COLUMN IF NOT EXISTS ws_dial_ms bigint NOT NULL DEFAULT 0;

	-- 小时汇总同样直接改名保留历史 FRT，再新建 TTFT 汇总列。
	ALTER TABLE public.usage_hourly_rollups
		RENAME COLUMN first_token_requests TO first_event_requests;

	ALTER TABLE public.usage_hourly_rollups
		RENAME COLUMN first_token_ms TO first_event_ms;

	ALTER TABLE public.usage_hourly_rollups
		ADD COLUMN first_token_requests bigint NOT NULL DEFAULT 0;

	ALTER TABLE public.usage_hourly_rollups
		ADD COLUMN first_token_ms bigint NOT NULL DEFAULT 0;
END;
$$;
