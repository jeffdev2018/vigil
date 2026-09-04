-- Module ownership (K33): a rule maps a path pattern or a label to an owner
-- member and an optional referent agent. Rules only suggest; assignment
-- stays a human click. No foreign keys by house rule; purged in
-- workspace_delete.sql; owner/agent existence is checked in the handler.
CREATE TABLE IF NOT EXISTS module_ownership (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    path_pattern      TEXT,
    label_id          UUID,
    owner_user_id     UUID NOT NULL,
    referent_agent_id UUID,
    priority          INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (path_pattern IS NOT NULL OR label_id IS NOT NULL)
);
