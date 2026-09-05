-- Working set for the Brain listing and for the run-time brief injection:
-- one workspace's notes, most recently updated first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_workspace_updated
    ON workspace_note (workspace_id, updated_at DESC);
