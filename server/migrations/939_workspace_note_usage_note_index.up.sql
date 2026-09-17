-- A note's usage summary and its most recent runs.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_usage_note_created
    ON workspace_note_usage (note_id, created_at DESC);
