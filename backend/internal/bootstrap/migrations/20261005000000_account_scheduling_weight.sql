-- description: Configure load-aware weighted account selection.
-- Ent auto-migration runs first; the versioned upgrade also enforces the DB range.
ALTER TABLE public.accounts
    ADD COLUMN IF NOT EXISTS scheduling_weight integer NOT NULL DEFAULT 100;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'public.accounts'::regclass
          AND conname = 'account_scheduling_weight_range'
    ) THEN
        ALTER TABLE public.accounts
            ADD CONSTRAINT account_scheduling_weight_range
            CHECK (scheduling_weight BETWEEN 0 AND 1000000);
    END IF;
END $$;
