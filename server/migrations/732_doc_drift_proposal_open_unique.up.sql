-- One OPEN proposal per (repository, document): a second scan that finds more
-- drift merges into the row already under review. Partial on purpose —
-- dismissed and merged proposals are history and must never block a future
-- detection, which is what makes "dismiss" a real answer rather than a mute.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_doc_drift_proposal_open
    ON doc_drift_proposal (workspace_id, repo_identifier, doc_path)
    WHERE status IN ('draft', 'opened_pr');
