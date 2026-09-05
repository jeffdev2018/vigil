CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_org_offer_issue
    ON org_offer (issue_id, created_at DESC);
