-- description: Add observed daily growth state for account usage windows.

ALTER TABLE public.accounts
	ADD COLUMN IF NOT EXISTS usage_5h_growth_date text NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS usage_5h_daily_growth double precision NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS usage_5h_last_percent double precision NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS usage_7d_growth_date text NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS usage_7d_daily_growth double precision NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS usage_7d_last_percent double precision NOT NULL DEFAULT 0;
