CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_code_wiki_snapshot_workspace
    ON code_wiki_snapshot (workspace_id);
