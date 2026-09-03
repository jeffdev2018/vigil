-- F17: the plan is a versioned artifact on the issue, published by the agent
-- (multica issue plan set) or a member. Each publication is a new row with the
-- next version; the previous active row gets superseded_at. Shaped after
-- issue_source_context (407): no FK, no cascade, cleanup in workspace_delete.
CREATE TABLE IF NOT EXISTS issue_plan (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    issue_id      UUID NOT NULL,
    version       INTEGER NOT NULL CHECK (version > 0),
    content       TEXT NOT NULL,
    -- Optional structured steps: [{id, title, done?}] as the agent wrote them.
    steps         JSONB NOT NULL DEFAULT '[]'::jsonb,
    author_type   TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id     UUID NOT NULL,
    superseded_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE issue_plan IS
    'Versioned plan artifact per issue (F17). The active plan is the row with superseded_at IS NULL; older versions stay readable. No FK by house rule.';
