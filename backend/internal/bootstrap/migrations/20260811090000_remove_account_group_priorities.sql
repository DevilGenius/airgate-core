-- description: Remove deprecated per-group account priority overrides.

UPDATE public.accounts
SET extra = extra - 'group_priorities'
WHERE extra ? 'group_priorities';
