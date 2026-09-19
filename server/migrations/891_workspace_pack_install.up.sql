-- Packs (OS plan, vague B): a pack is a transfer bundle with a manifest. The
-- install ledger records which pack, which version and which rows it created,
-- so a pack can be upgraded in place or uninstalled without guessing.
CREATE TABLE workspace_pack_install (
    id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    pack_id TEXT NOT NULL,
    pack_version TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL CHECK (source IN ('builtin', 'upload', 'workspace')),
    strategy TEXT NOT NULL DEFAULT '',
    bundle_sha256 TEXT NOT NULL DEFAULT '',
    run_id UUID,
    status TEXT NOT NULL DEFAULT 'installed' CHECK (status IN ('installed', 'failed', 'removed')),
    report JSONB NOT NULL DEFAULT '{}'::jsonb,
    manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    installed_by UUID,
    installed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    removed_by UUID,
    removed_at TIMESTAMPTZ
);

CREATE TABLE workspace_pack_item (
    id UUID PRIMARY KEY,
    install_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL,
    row_id UUID,
    name TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL CHECK (action IN ('created', 'merged', 'skipped')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
