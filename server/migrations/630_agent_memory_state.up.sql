-- Agent memory governance (JEF-269): every fact carries a review state.
-- Human-written rows ('manual') and postmortem rules are 'approved'; facts
-- the post-run extraction pass learned on its own land as 'draft' until a
-- human approves them over REST. The DEFAULT covers existing rows — they
-- were all human-approved or human-reviewed at write time, so none are
-- retrograded.
ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS state TEXT NOT NULL DEFAULT 'approved'
    CHECK (state IN ('draft', 'approved'));
