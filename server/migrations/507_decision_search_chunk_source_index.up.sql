CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_decision_search_chunk_source ON decision_search_chunk (source_type, source_id);
