-- description: Delete legacy duplicate accounts and backfill normalized account emails.

BEGIN;

CREATE TEMP TABLE airgate_account_email_duplicate_map
ON COMMIT DROP
AS
WITH candidates AS (
	SELECT
		id,
		LOWER(COALESCE(
			NULLIF(BTRIM(email), ''),
			NULLIF(BTRIM(credentials->>'email'), '')
		)) AS normalized_email,
		state,
		last_used_at,
		updated_at
	FROM public.accounts
), ranked AS (
	SELECT
		c.*,
		FIRST_VALUE(c.id) OVER identity_order AS canonical_id,
		ROW_NUMBER() OVER identity_order AS identity_rank
	FROM candidates AS c
	WHERE c.normalized_email IS NOT NULL
	WINDOW identity_order AS (
		PARTITION BY c.normalized_email
		ORDER BY
			(c.state <> 'disabled') DESC,
			c.last_used_at DESC NULLS LAST,
			c.updated_at DESC,
			c.id DESC
	)
)
SELECT
	id AS duplicate_id,
	canonical_id
FROM ranked
WHERE identity_rank > 1;

CREATE UNIQUE INDEX ON airgate_account_email_duplicate_map (duplicate_id);
ANALYZE airgate_account_email_duplicate_map;

DO $preupgrade_check$
BEGIN
	IF EXISTS (SELECT 1 FROM airgate_account_email_duplicate_map) THEN
		IF EXISTS (
			SELECT 1
			FROM public.usage_logs AS usage_log
			JOIN airgate_account_email_duplicate_map AS duplicate_map
				ON duplicate_map.duplicate_id = usage_log.account_usage_logs
		) THEN
			RAISE EXCEPTION 'account email migration requires manual usage_log relink before restart';
		END IF;
	END IF;
END;
$preupgrade_check$;

INSERT INTO public.account_groups (account_id, group_id)
SELECT DISTINCT duplicate_map.canonical_id, account_group.group_id
FROM airgate_account_email_duplicate_map AS duplicate_map
JOIN public.account_groups AS account_group
	ON account_group.account_id = duplicate_map.duplicate_id
ON CONFLICT DO NOTHING;

WITH proxy_sources AS (
	SELECT DISTINCT ON (duplicate_map.canonical_id)
		duplicate_map.canonical_id,
		duplicate.account_proxy
	FROM airgate_account_email_duplicate_map AS duplicate_map
	JOIN public.accounts AS duplicate
		ON duplicate.id = duplicate_map.duplicate_id
	WHERE duplicate.account_proxy IS NOT NULL
	ORDER BY duplicate_map.canonical_id, duplicate_map.duplicate_id DESC
)
UPDATE public.accounts AS canonical
SET account_proxy = proxy_sources.account_proxy
FROM proxy_sources
WHERE canonical.id = proxy_sources.canonical_id
	AND canonical.account_proxy IS NULL;

DELETE FROM public.accounts AS account
USING airgate_account_email_duplicate_map AS duplicate_map
WHERE account.id = duplicate_map.duplicate_id;

UPDATE public.accounts
SET email = LOWER(COALESCE(
	NULLIF(BTRIM(email), ''),
	NULLIF(BTRIM(credentials->>'email'), '')
))
WHERE email IS DISTINCT FROM LOWER(COALESCE(
	NULLIF(BTRIM(email), ''),
	NULLIF(BTRIM(credentials->>'email'), '')
));

UPDATE public.accounts
SET email = NULL,
	credentials = credentials - 'email'
WHERE NULLIF(BTRIM(email), '') IS NULL
	AND NULLIF(BTRIM(credentials->>'email'), '') IS NULL
	AND (email IS NOT NULL OR credentials ? 'email');

UPDATE public.accounts
SET credentials = jsonb_set(
	COALESCE(credentials, '{}'::jsonb),
	'{email}',
	to_jsonb(email),
	true
)
WHERE email IS NOT NULL
	AND credentials->>'email' IS DISTINCT FROM email;

COMMIT;
