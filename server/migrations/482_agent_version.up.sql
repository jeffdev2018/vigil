-- Agent versions (K23): an immutable snapshot of what an agent is
-- (instructions, model, enabled skills, tool configuration) taken after
-- every change, numbered per agent. The agent row stays the live config;
-- the newest version is the active one. No foreign keys by house rule;
-- purged with the workspace.
CREATE TABLE IF NOT EXISTS agent_version (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    agent_id        UUID NOT NULL,
    version_number  INTEGER NOT NULL CHECK (version_number > 0),
    instructions    TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    skill_ids       JSONB NOT NULL DEFAULT '[]',
    tool_config     JSONB NOT NULL DEFAULT '{}',
    note            TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL DEFAULT 'system' CHECK (created_by_type IN ('member', 'agent', 'system')),
    created_by_id   UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
