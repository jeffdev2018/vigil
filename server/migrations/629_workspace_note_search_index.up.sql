-- Full-text search over title + body. 'simple' (no stemming, no stopword
-- list) because the corpus is polyglot engineering prose: an English stemmer
-- would mangle identifiers and drop CJK entirely.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_search
    ON workspace_note USING GIN (to_tsvector('simple', title || ' ' || content));
