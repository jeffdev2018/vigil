-- Workspace doctrine (OS plan, chantier 22): workspace.context grown up.
-- The live text stays in workspace.context so every daemon claim keeps
-- reading it; this migration adds the revision ledger, the review flow and
-- the reports agents file when a task collides with a rule.
ALTER TABLE workspace ADD COLUMN doctrine_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE workspace ADD COLUMN doctrine_updated_at TIMESTAMPTZ;
ALTER TABLE workspace ADD COLUMN doctrine_updated_by UUID;

CREATE TABLE workspace_doctrine_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    -- Assigned when the version becomes active; NULL while pending or once rejected.
    revision INTEGER CHECK (revision >= 0),
    content TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'pending', 'rejected', 'superseded')),
    note TEXT NOT NULL DEFAULT '',
    author_id UUID,
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    review_note TEXT NOT NULL DEFAULT '',
    restored_from_revision INTEGER CHECK (restored_from_revision >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Existing contexts become revision 0 so history starts where the workspace is.
INSERT INTO workspace_doctrine_version (workspace_id, revision, content, status, created_at)
SELECT id, 0, context, 'active', updated_at FROM workspace WHERE context IS NOT NULL AND btrim(context) <> '';

CREATE TABLE workspace_doctrine_report (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    doctrine_revision INTEGER NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('conflict', 'refusal', 'ambiguity')),
    summary TEXT NOT NULL,
    passage TEXT NOT NULL DEFAULT '',
    reporter_type TEXT NOT NULL CHECK (reporter_type IN ('agent', 'member')),
    reporter_id UUID NOT NULL,
    task_id UUID,
    issue_id UUID,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'dismissed')),
    resolved_by UUID,
    resolved_at TIMESTAMPTZ,
    resolution_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
