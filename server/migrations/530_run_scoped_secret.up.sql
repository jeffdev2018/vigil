-- Run-scoped secrets (K09): an agent env key marked as scoped is never
-- handed to a run in clear. The claim issues an opaque token in its place,
-- valid for this run only and revoked at any terminal status; the daemon's
-- MCP broker swaps the token for the value on the way out, so neither the
-- agent process nor the daemon disk ever holds the secret.
CREATE TABLE run_scoped_secret (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    task_id      UUID NOT NULL,
    agent_id     UUID NOT NULL,
    key          TEXT NOT NULL,
    token_hash   TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    revoke_reason TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE agent ADD COLUMN IF NOT EXISTS scoped_env_keys JSONB NOT NULL DEFAULT '[]'::jsonb;
