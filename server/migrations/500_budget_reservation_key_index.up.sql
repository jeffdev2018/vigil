CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_budget_reservation_key ON budget_reservation(policy_id, period_start, period_end, idempotency_key);
