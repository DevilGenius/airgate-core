-- description: Retain request traces until manual removal; retire expiration metadata.

-- Dropping this column also removes its retention index. No trace payloads or
-- occurrence events are deleted, including rows with an old expiration date.
ALTER TABLE IF EXISTS public.monitor_request_trace DROP COLUMN IF EXISTS expires_at;
