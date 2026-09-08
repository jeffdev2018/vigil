ALTER TABLE project ADD COLUMN memory_expires_at TIMESTAMPTZ;

CREATE TABLE project_memory_version (
    project_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 0),
    rules JSONB NOT NULL CHECK (jsonb_typeof(rules) = 'array' AND jsonb_array_length(rules) <= 20),
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    restored_from_revision INTEGER CHECK (restored_from_revision >= 0)
);

INSERT INTO project_memory_version (project_id, workspace_id, revision, rules, reviewed_by, reviewed_at, expires_at)
SELECT id, workspace_id, memory_revision, memory_rules, memory_reviewed_by, memory_reviewed_at, memory_expires_at FROM project;
