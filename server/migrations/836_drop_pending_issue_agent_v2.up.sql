-- v3 (migration 835) now carries the pending-slot rule for every ungrouped row.
-- v2 stays until here so the rule is never unenforced, and is dropped rather
-- than kept because it is what blocks a group's second attempt by the same
-- agent from being inserted at all.
DROP INDEX CONCURRENTLY IF EXISTS idx_one_pending_task_per_issue_agent_v2;
