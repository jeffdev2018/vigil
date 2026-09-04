CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_budget_period_scope ON budget_period(policy_id, period_start, period_end);
