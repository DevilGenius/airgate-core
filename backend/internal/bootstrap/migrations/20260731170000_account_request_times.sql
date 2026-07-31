-- description: Split account access and health-probe timestamps.

ALTER TABLE public.accounts
	ADD COLUMN IF NOT EXISTS last_probe_at timestamptz NULL;

UPDATE public.accounts AS account
SET last_used_at = (
	SELECT MAX(usage_log.created_at)
	FROM public.usage_logs AS usage_log
	WHERE usage_log.account_usage_logs = account.id
)
WHERE account.deleted_at IS NULL;
