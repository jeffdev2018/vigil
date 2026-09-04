-- Handoff packets (K17): the structured record a run leaves behind —
-- objective, decisions, evidence, failed attempts, next action. Immutable:
-- a correction is a new packet. handoff_note stays on agent_task_queue for
-- installed clients; the packet is the artefact.
CREATE TABLE handoff_packet (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL,
    workspace_id    UUID NOT NULL,
    issue_id        UUID NOT NULL,
    objective       TEXT NOT NULL,
    decisions       JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence        JSONB NOT NULL DEFAULT '[]'::jsonb,
    failed_attempts JSONB NOT NULL DEFAULT '[]'::jsonb,
    next_action     TEXT,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('agent', 'member', 'system')),
    created_by_id   UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
