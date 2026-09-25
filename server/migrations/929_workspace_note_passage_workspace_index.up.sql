-- Workspace teardown and the per-workspace lexical prefilter.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_passage_workspace
    ON workspace_note_passage (workspace_id);
