-- K60 · SSO (OIDC) per workspace, SCIM provisioning with immediate session
-- revocation, and project roles that only restrict the workspace role.
CREATE TABLE IF NOT EXISTS workspace_sso_connection (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL UNIQUE,
    provider TEXT NOT NULL DEFAULT 'oidc' CHECK (provider IN ('oidc')),
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret_encrypted TEXT NOT NULL,
    allowed_domains JSONB NOT NULL DEFAULT '[]'::jsonb,
    auto_provision BOOLEAN NOT NULL DEFAULT true,
    enforced BOOLEAN NOT NULL DEFAULT false,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS scim_token (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    token_hash TEXT NOT NULL,
    token_hint TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT true,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS project_member_role (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('member', 'agent')),
    subject_id UUID NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('viewer', 'contributor', 'admin')),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, subject_type, subject_id)
);
-- A JWT issued before this instant is refused: the synchronous revocation
-- SCIM promises.
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS sessions_invalidated_at TIMESTAMPTZ;
ALTER TABLE member ADD COLUMN IF NOT EXISTS scim_external_id TEXT;
