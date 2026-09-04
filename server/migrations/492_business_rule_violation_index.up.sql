CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_business_rule_violation_rule ON business_rule_violation (rule_id, created_at DESC);
