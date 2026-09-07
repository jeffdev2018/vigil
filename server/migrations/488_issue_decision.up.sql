CREATE TABLE issue_decision (
    id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    source_task_id UUID NOT NULL,
    recipient_id UUID NOT NULL,
    requested_by UUID NOT NULL,
    requester_type TEXT NOT NULL CHECK (requester_type IN ('member', 'agent')),
    question TEXT NOT NULL CHECK (length(question) BETWEEN 1 AND 1000),
    context TEXT NOT NULL CHECK (length(context) <= 8000),
    options JSONB NOT NULL CHECK (jsonb_typeof(options) = 'array' AND jsonb_array_length(options) <= 8),
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'cancelled')),
    answer TEXT CHECK (length(answer) BETWEEN 1 AND 4000),
    answered_by UUID,
    answered_at TIMESTAMPTZ,
    resume_task_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK ((status = 'open' AND answer IS NULL AND answered_by IS NULL AND answered_at IS NULL)
        OR (status = 'answered' AND answer IS NOT NULL AND answered_by IS NOT NULL AND answered_at IS NOT NULL)
        OR (status = 'cancelled' AND answer IS NULL AND answered_by IS NOT NULL AND answered_at IS NOT NULL)),
    CHECK (resume_task_id IS NULL OR status = 'answered')
);
