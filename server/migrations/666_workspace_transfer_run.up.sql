-- Workspace export / import (K76): every transfer leaves a run with its
-- report; a template export keeps its bundle so a new workspace can start
-- from it. Secrets never travel: the bundle carries declarations only.
CREATE TABLE IF NOT EXISTS workspace_transfer_run (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    direction TEXT NOT NULL CHECK (direction IN ('export', 'import')),
    status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('running', 'completed', 'failed')),
    name TEXT NOT NULL DEFAULT '',
    template BOOLEAN NOT NULL DEFAULT false,
    strategy TEXT NOT NULL DEFAULT '',
    source_name TEXT NOT NULL DEFAULT '',
    bundle_sha256 TEXT NOT NULL DEFAULT '',
    bundle BYTEA,
    report JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
