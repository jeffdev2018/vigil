CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_org_flow_structure
    ON org_flow (structure_id, created_at DESC);
