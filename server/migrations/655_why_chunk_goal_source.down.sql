DELETE FROM decision_search_chunk WHERE source_type = 'goal';
ALTER TABLE decision_search_chunk DROP CONSTRAINT IF EXISTS decision_search_chunk_source_type_check;
ALTER TABLE decision_search_chunk ADD CONSTRAINT decision_search_chunk_source_type_check
    CHECK (source_type IN ('comment', 'task_message', 'decision_record'));
