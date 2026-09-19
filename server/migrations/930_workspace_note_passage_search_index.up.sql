-- Full-text search over the folded title, heading path and body.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_passage_tsv
    ON workspace_note_passage USING GIN (tsv);
