-- Goals with ancestry (K74): a workspace mission is a root goal, sub-goals
-- hang under it, projects link to goals (n-n) and an issue may name a goal
-- directly (it inherits the project's goals otherwise). No foreign keys per
-- the repository migration rules; the handler resolves and cleans up.
CREATE TABLE IF NOT EXISTS goal (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    parent_goal_id UUID,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    success_measure TEXT NOT NULL DEFAULT '',
    due_date DATE,
    owner_id UUID,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'done', 'dropped')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_goal (
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    goal_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, goal_id)
);

ALTER TABLE issue ADD COLUMN IF NOT EXISTS goal_id UUID;
