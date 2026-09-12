CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_search
    ON workspace_note USING GIN (to_tsvector('simple', title || ' ' || content));
