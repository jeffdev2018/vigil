-- Cosine ANN index for the vector re-rank. pgvector >= 0.5 for hnsw; the repo
-- ships pgvector/pgvector:pg17 (0.8.x) in every compose file and CI, so the
-- build is unconditional. An index-less deployment would still answer, just by
-- sequential scan, which is why nothing downstream depends on this existing.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_repo_index_chunk_embedding
    ON repo_index_chunk USING hnsw (embedding vector_cosine_ops);
