CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_decision_search_chunk_tsv ON decision_search_chunk USING gin (tsv);
