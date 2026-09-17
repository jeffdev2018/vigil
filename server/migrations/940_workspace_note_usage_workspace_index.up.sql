-- Workspace teardown.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_usage_workspace
    ON workspace_note_usage (workspace_id);
