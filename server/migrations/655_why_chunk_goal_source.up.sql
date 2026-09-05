-- Goals (K74) are indexed for the "why" search alongside comments, run
-- messages and decision records.
ALTER TABLE decision_search_chunk DROP CONSTRAINT IF EXISTS decision_search_chunk_source_type_check;
ALTER TABLE decision_search_chunk ADD CONSTRAINT decision_search_chunk_source_type_check
    CHECK (source_type IN ('comment', 'task_message', 'decision_record', 'goal'));
