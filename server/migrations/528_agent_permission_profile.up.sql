-- Permission profiles (K06): what an agent may touch when it runs. Five
-- builtin profiles are seeded per workspace on first read; an agent carries
-- one, a run may override it. Enforcement happens at claim time (secrets
-- filtered, provider flags), at the approval gates (denied paths) and in the
-- prompt (allowed commands).
CREATE TABLE agent_permission_profile (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    read_only        BOOLEAN NOT NULL DEFAULT false,
    denied_paths     JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_commands JSONB NOT NULL DEFAULT '[]'::jsonb,
    hidden_secrets   JSONB NOT NULL DEFAULT '[]'::jsonb,
    builtin          BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE agent ADD COLUMN IF NOT EXISTS permission_profile_id UUID;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS permission_profile_id UUID;
