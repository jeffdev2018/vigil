-- Undo for agent actions (K69): every side effect an agent run produces
-- through the API is journaled with enough of the "before" state to reverse
-- it. Reversal happens in application code, effect by effect, within the
-- workspace's undo window. No foreign keys (house rule): task_id points at
-- agent_task_queue, agent_id at agent, issue_id at issue, target_id at the
-- row the kind names (issue, comment, workspace_note, triage_item).
CREATE TABLE IF NOT EXISTS agent_effect (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    task_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    issue_id UUID,
    kind TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    before JSONB NOT NULL DEFAULT '{}'::jsonb,
    after JSONB NOT NULL DEFAULT '{}'::jsonb,
    reversible BOOLEAN NOT NULL DEFAULT true,
    reversed_at TIMESTAMPTZ,
    reversed_by_type TEXT,
    reversed_by_id UUID,
    reverse_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(before) = 'object'),
    CHECK (jsonb_typeof(after) = 'object')
);
