-- "Show me first" (K69, lot 2): an agent in preview mode does not apply its
-- writes; they are journaled as pending effects, listed in one decision at
-- the end of the run, and applied only when a human approves.
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS effect_mode TEXT NOT NULL DEFAULT 'apply'
        CHECK (effect_mode IN ('apply', 'preview'));

ALTER TABLE agent_effect
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'applied'
        CHECK (status IN ('applied', 'pending', 'approved', 'rejected')),
    ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS decision_id UUID;

COMMENT ON COLUMN agent_effect.payload IS 'The write a pending effect will replay on approval (request fields), empty for an applied effect.';
