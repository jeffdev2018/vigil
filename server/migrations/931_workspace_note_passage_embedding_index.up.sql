-- Nearest-passage search for the semantic leg; rows without a vector are
-- not indexed.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_passage_embedding
    ON workspace_note_passage USING hnsw (embedding vector_cosine_ops);
