-- Agent review by agent (JEF-238): per-project review policy layered on the
-- cross-provider review (K15). One row per project: a checklist the reviewer
-- must verify, an optional pinned reviewer agent, and a done gate that sends
-- a request_changes verdict back to the worker, up to max_cycles times.
-- No foreign keys (repo convention); dependent cleanup is application-side.
CREATE TABLE IF NOT EXISTS project_review_config (
    project_id        UUID NOT NULL,
    workspace_id      UUID NOT NULL,
    checklist         JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(checklist) = 'array'),
    reviewer_agent_id UUID,
    gate_enabled      BOOLEAN NOT NULL DEFAULT false,
    max_cycles        INTEGER NOT NULL DEFAULT 3,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
