-- description: Add per-account model success-rate downgrade threshold.

ALTER TABLE public.accounts
	ADD COLUMN IF NOT EXISTS model_downgrade_threshold double precision NOT NULL DEFAULT 0;

UPDATE public.accounts
SET model_downgrade_threshold = 0
WHERE model_downgrade_threshold IS NULL;

ALTER TABLE public.accounts
	ALTER COLUMN model_downgrade_threshold SET DEFAULT 0,
	ALTER COLUMN model_downgrade_threshold SET NOT NULL;

DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = 'public'
			AND t.relname = 'accounts'
			AND c.conname = 'account_model_downgrade_threshold_range'
	) THEN
		ALTER TABLE public.accounts
			ADD CONSTRAINT account_model_downgrade_threshold_range
			CHECK (model_downgrade_threshold >= 0 AND model_downgrade_threshold <= 1);
	END IF;
END $$;
