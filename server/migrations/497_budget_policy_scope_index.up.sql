CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_budget_policy_scope ON budget_policy(workspace_id, scope_type, scope_id) NULLS NOT DISTINCT;
