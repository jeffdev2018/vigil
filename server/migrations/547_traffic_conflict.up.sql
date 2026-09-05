-- Traffic control (K18): the paths a run edits (from its tool calls) are
-- compared with what other active runs edit and with what a human has
-- modified in the daemon's local checkouts (reported on the heartbeat). An
-- overlap is a conflict: an Attention Inbox item, optionally a pause. No
-- lock is taken: the human decides.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS touched_paths JSONB;
CREATE TABLE traffic_conflict (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    issue_id          UUID NOT NULL,
    task_id           UUID NOT NULL,
    kind              TEXT NOT NULL CHECK (kind IN ('human', 'agent')),
    paths             JSONB NOT NULL DEFAULT '[]'::jsonb,
    other_task_id     UUID,
    handoff_packet_id UUID,
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'ignored', 'resolved')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ
);
