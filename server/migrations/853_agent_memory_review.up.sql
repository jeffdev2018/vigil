ALTER TABLE agent_memory
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'rejected')),
    ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN reviewed_by UUID,
    ADD COLUMN reviewed_at TIMESTAMPTZ;

-- Automatically extracted facts have never been reviewed by a human.
-- Preserve their content and provenance, but require approval before reuse.
UPDATE agent_memory SET status = 'pending' WHERE source = 'run';
