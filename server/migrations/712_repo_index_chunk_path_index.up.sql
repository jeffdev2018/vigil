-- Backing index for every hot path: the incremental diff (DISTINCT file_path /
-- content_hash per repo), the per-file atomic replace, the prune, the per-repo
-- stats and the lexical prefilter's repo scope. Own single-statement migration
-- so CONCURRENTLY runs outside an implicit transaction (repo convention).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_repo_index_chunk_path
    ON repo_index_chunk (workspace_id, repo_identifier, file_path);
