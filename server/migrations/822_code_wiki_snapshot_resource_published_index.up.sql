CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_code_wiki_snapshot_resource_published
    ON code_wiki_snapshot (project_resource_id, published_at DESC);
