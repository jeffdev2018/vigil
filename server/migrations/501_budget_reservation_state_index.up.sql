CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_budget_reservation_reserved ON budget_reservation(state, created_at) WHERE state = 'reserved';
