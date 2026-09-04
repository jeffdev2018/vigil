CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_budget_override_policy_expiry ON budget_override(policy_id, expires_at DESC);
