CREATE TABLE issue_delivery_contract (
    issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    criteria JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(criteria) = 'array'),
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    updated_by UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE issue_delivery_review (
    id UUID NOT NULL,
    issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    task_id UUID NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('accepted', 'changes_requested')),
    feedback TEXT NOT NULL,
    assessments JSONB NOT NULL CHECK (jsonb_typeof(assessments) = 'array'),
    snapshot JSONB NOT NULL,
    snapshot_token TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    reviewed_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
