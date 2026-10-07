-- description: Persist account naming counters independently of account records.
CREATE TABLE IF NOT EXISTS public.account_name_counters (
    platform text NOT NULL,
    naming_date date NOT NULL,
    plan_type text NOT NULL,
    last_index bigint NOT NULL DEFAULT 0 CHECK (last_index >= 0),
    PRIMARY KEY (platform, naming_date, plan_type)
);
