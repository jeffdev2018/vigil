-- K01: a Decision Card is a typed question an agent asks a human on an issue:
-- options with impact, a recommendation, an urgency. The answer is stored on
-- the same row and a new run is queued with it in the handoff note. Its own
-- table rather than a task_message row: task_message.seq is assigned by the
-- daemon, and a server-side insert would race that ordering. No FK, cleanup in
-- workspace_delete.sql.
CREATE TABLE IF NOT EXISTS issue_decision (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL,
    issue_id              UUID NOT NULL,
    -- The run that asked; NULL when a human filed the question by hand.
    task_id               UUID,
    asked_by_type         TEXT NOT NULL CHECK (asked_by_type IN ('member', 'agent')),
    asked_by_id           UUID NOT NULL,
    question              TEXT NOT NULL,
    -- [{id, label, impact?}], at least two entries, ids unique.
    options               JSONB NOT NULL,
    recommended_option_id TEXT,
    urgency               TEXT NOT NULL DEFAULT 'normal' CHECK (urgency IN ('low', 'normal', 'high')),
    -- {option_id?, modified_text?}; NULL while pending.
    response              JSONB,
    responded_by_type     TEXT CHECK (responded_by_type IN ('member', 'agent')),
    responded_by_id       UUID,
    responded_at          TIMESTAMPTZ,
    -- The run queued with the answer, when the issue is agent-assigned.
    resume_task_id        UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((response IS NULL AND responded_at IS NULL) OR (response IS NOT NULL AND responded_at IS NOT NULL))
);

COMMENT ON TABLE issue_decision IS
    'Decision Cards (K01): a typed question from an agent to a human on an issue, with options, recommendation, urgency and the recorded answer. No FK by house rule.';
