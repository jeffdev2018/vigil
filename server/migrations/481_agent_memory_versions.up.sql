CREATE TABLE agent_memory_version (
    memory_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    content TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 500),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'rejected')),
    source TEXT NOT NULL,
    source_task_id UUID,
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    restored_from_revision INTEGER CHECK (restored_from_revision > 0)
);

INSERT INTO agent_memory_version (memory_id, workspace_id, agent_id, revision, content, status, source, source_task_id, reviewed_by, reviewed_at, created_at, updated_at, expires_at)
SELECT id, workspace_id, agent_id, revision, content, status, source, source_task_id, reviewed_by, reviewed_at, created_at, updated_at, expires_at FROM agent_memory;
