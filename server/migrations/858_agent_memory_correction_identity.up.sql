CREATE UNIQUE INDEX CONCURRENTLY agent_memory_correction_identity ON agent_memory (agent_id, (source_review->>'review_id')) WHERE source_review IS NOT NULL;
