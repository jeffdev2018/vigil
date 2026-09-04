CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_business_rule_attach_point ON business_rule (workspace_id, attach_point, status);
