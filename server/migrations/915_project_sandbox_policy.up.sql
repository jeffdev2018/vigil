-- JEF-256: one declarative sandbox policy per project, layered over the
-- workspace default (workspace.settings->'sandbox_policy') and under the
-- per-issue override (issue.metadata->'sandbox_policy'). The server resolves
-- the chain at claim time — most-restrictive wins — and merges the result
-- into the SandboxSpec the claim already carries.
--
-- Row present = explicit policy; deleting the row returns the project to the
-- workspace default. The primary key is attached by 916/917 through a
-- CONCURRENTLY-built index, per the repository rule that no index is built
-- inside a multi-statement migration.
--
-- No foreign keys and no cascades, per the repository rule: workspace
-- teardown removes these rows in DeleteWorkspaceRuntimesAndProjects before
-- the project rows go.
CREATE TABLE IF NOT EXISTS project_sandbox_policy (
    project_id            UUID NOT NULL,
    network_mode          TEXT NOT NULL,
    allowed_hosts         JSONB NOT NULL DEFAULT '[]'::jsonb,
    block_sensitive_files BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE project_sandbox_policy IS
    'JEF-256: per-project sandbox policy (network mode, host allowlist, sensitive-file block), merged most-restrictive-first into the claim''s SandboxSpec.';
COMMENT ON COLUMN project_sandbox_policy.network_mode IS
    'unrestricted | allowlist | none. allowlist is the only mode whose allowed_hosts carry meaning; the handler rejects hosts under any other mode.';
