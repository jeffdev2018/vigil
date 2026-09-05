-- Postmortem autogen (k68): after a run fails terminally, the system drafts
-- a postmortem (summary, root cause, impact, preventive rules) — via the
-- assist-layer LLM when configured, else a deterministic scaffold. An item is
-- an artifact humans review; approve keeps it, discard drops it. One
-- postmortem per failed task (enforced by 469).
CREATE TABLE postmortem (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    source_task_id UUID NOT NULL,
    issue_id UUID,
    agent_id UUID,
    trigger TEXT NOT NULL DEFAULT 'failed' CHECK (trigger IN ('failed', 'costly')),
    state TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'approved', 'discarded')),
    failure_reason TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    root_cause TEXT NOT NULL DEFAULT '',
    impact TEXT NOT NULL DEFAULT '',
    preventive_rules JSONB NOT NULL DEFAULT '[]'::jsonb,
    cost_usd_ticks BIGINT,
    llm_generated BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_at TIMESTAMPTZ,
    resolved_by_type TEXT CHECK (resolved_by_type IN ('member', 'agent', 'system')),
    resolved_by_id UUID,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (state = 'draft' AND resolved_at IS NULL)
        OR (state IN ('approved', 'discarded') AND resolved_at IS NOT NULL)
    ),
    CHECK (jsonb_typeof(preventive_rules) = 'array')
);

COMMENT ON COLUMN postmortem.source_task_id IS
    'The failed agent run (agent_task_queue.id) this postmortem analyzes.';
COMMENT ON COLUMN postmortem.llm_generated IS
    'true when drafted by the assist-layer LLM, false for the deterministic scaffold.';
