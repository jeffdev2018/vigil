-- Decision memory (K29): the decisions an accepted run stated, each pointing
-- at the run message that states it. A structured read layer over
-- task_message, not a second log: no decision without a source seq.
CREATE TABLE decision_record (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID,
    issue_id UUID NOT NULL,
    run_id UUID NOT NULL,
    source_message_seq INTEGER NOT NULL,
    title TEXT NOT NULL,
    context TEXT NOT NULL,
    decision TEXT NOT NULL,
    consequences TEXT,
    author_type TEXT NOT NULL CHECK (author_type IN ('agent', 'member')),
    author_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
