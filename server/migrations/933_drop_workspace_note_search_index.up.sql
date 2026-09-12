-- JEF-412: Brain search reads the folded per-passage tsvector
-- (idx_workspace_note_passage_tsv). No query ranks or filters on
-- to_tsvector('simple', title || ' ' || content) any more, so its index only
-- slows every note write.
DROP INDEX CONCURRENTLY IF EXISTS idx_workspace_note_search;
