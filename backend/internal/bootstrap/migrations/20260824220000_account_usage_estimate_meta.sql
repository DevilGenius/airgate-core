-- description: Add account usage estimate metadata.

ALTER TABLE public.accounts
	ADD COLUMN IF NOT EXISTS usage_estimate_meta jsonb NULL DEFAULT '{}'::jsonb;

UPDATE public.accounts
SET usage_estimate_meta = '{}'::jsonb
WHERE usage_estimate_meta IS NULL;

ALTER TABLE public.accounts
	ALTER COLUMN usage_estimate_meta SET DEFAULT '{}'::jsonb;
