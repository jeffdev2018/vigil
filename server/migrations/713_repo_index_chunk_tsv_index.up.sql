-- Full-text prefilter. Lexical is the primary ranking signal: it is the only
-- one a deployment without an embeddings endpoint has.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_repo_index_chunk_tsv
    ON repo_index_chunk USING GIN (tsv);
