-- One triage_source per (workspace, kind, source object). The capture path
-- upserts on this key; it is also the key a future mode flip (gate/block)
-- addresses a source by.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_triage_source_workspace_kind_ref
    ON triage_source (workspace_id, kind, ref_id);
