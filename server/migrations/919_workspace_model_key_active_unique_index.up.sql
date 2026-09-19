-- Audit finding: createModelKey's "one active key per vendor/scope" invariant
-- was enforced by a read-then-write check in Go (ListModelKeys, then compare
-- in memory) with no DB constraint behind it, so two concurrent creates for
-- the same workspace+provider+scope+scope_id could both pass the check and
-- both insert, leaving two active rows where resolveModelKeyForClaim and the
-- UI assume exactly one. COALESCE folds the NULL scope_id of workspace-scope
-- rows into a single value so they collide too, not just project-scope rows.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_model_key_active_unique
    ON workspace_model_key (workspace_id, provider, scope, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE active;
