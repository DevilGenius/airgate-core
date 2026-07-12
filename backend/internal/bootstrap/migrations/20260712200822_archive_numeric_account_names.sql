-- description: Archive legacy accounts with numeric names from 1 through 200.

UPDATE public.accounts
SET name = 'ArchivedCredential',
	updated_at = NOW()
WHERE name IN (
	SELECT numeric_name::text
	FROM generate_series(1, 200) AS generated(numeric_name)
);
