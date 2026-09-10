-- v3 (migration 837) carried the pending-slot rule for ungrouped rows while
-- the thread dimension did not exist. Migration 895 now carries the same rule
-- with the thread column added, so v3 has become the stricter of two indexes
-- covering one rule: it ignores comment_thread_id and would refuse the second
-- thread's task that upstream's change exists to allow.
--
-- Dropped after 895 is built, never before, so the rule is never unenforced.
DROP INDEX CONCURRENTLY IF EXISTS idx_one_pending_task_per_issue_agent_v3;
