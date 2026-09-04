CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_run_limit_policy_scope ON run_limit_policy (workspace_id, scope_type, scope_id) NULLS NOT DISTINCT;
