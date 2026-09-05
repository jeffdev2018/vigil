-- Skill Miner (K58): a member comment closely following an agent comment on
-- the same issue is a correction signal; recurring similar signals become a
-- DRAFT skill for review. Drafts are never assignable until published.
CREATE TABLE IF NOT EXISTS agent_correction_signal (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    agent_comment_id UUID NOT NULL,
    correction_comment_id UUID NOT NULL,
    status_regressed BOOLEAN NOT NULL DEFAULT false,
    mined_skill_id UUID,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (correction_comment_id)
);

ALTER TABLE skill ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'published'
    CHECK (status IN ('draft', 'published'));
