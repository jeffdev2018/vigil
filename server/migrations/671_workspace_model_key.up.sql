-- K48 · BYOK model keys per workspace or project, resolved at claim and
-- attributed to the run's usage. Rotation keeps the old row (inactive) so
-- historical usage still points at the key that paid for it.
CREATE TABLE IF NOT EXISTS workspace_model_key (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('workspace', 'project')),
    scope_id UUID,
    provider TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    key_encrypted TEXT NOT NULL,
    key_hint TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT true,
    priority INT NOT NULL DEFAULT 0,
    deactivated_reason TEXT NOT NULL DEFAULT '',
    deactivated_at TIMESTAMPTZ,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workspace_model_key_scope_id_matches CHECK (
        (scope = 'workspace' AND scope_id IS NULL) OR (scope = 'project' AND scope_id IS NOT NULL)
    )
);
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS model_key_id UUID;
ALTER TABLE task_usage ADD COLUMN IF NOT EXISTS model_key_id UUID;
