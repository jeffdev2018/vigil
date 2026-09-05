-- The inbound email endpoint looks a source up by token digest alone, so the
-- digest must be indexed and unique. Partial: every non-email source keeps the
-- empty default and must not collide with the others.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_triage_source_token_hash
    ON triage_source (token_hash) WHERE token_hash <> '';
