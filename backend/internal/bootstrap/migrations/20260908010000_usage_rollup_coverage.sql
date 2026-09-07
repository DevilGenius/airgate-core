-- description: Track verified historical coverage before serving usage rollups.

CREATE TABLE IF NOT EXISTS public.usage_rollup_coverage (
    projection text PRIMARY KEY,
    version integer NOT NULL DEFAULT 1,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'ready', 'failed')),
    covered_from timestamptz,
    verified_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Existence or nonzero contents do not prove a historical backfill completed.
-- Only complete future UTC buckets are known to be maintained by live writers.
INSERT INTO public.usage_rollup_coverage (projection, covered_from)
VALUES
    ('usage_hourly_rollups', date_trunc('hour', now()) + interval '1 hour'),
    ('usage_api_key_hourly_rollups', date_trunc('hour', now()) + interval '1 hour')
ON CONFLICT (projection) DO NOTHING;
