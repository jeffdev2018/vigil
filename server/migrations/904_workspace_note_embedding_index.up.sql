CREATE INDEX CONCURRENTLY idx_workspace_note_embedding ON workspace_note_embedding USING hnsw (embedding vector_cosine_ops);
