-- Index behind the per-issue A2A hourly budget (F19 / JEF-32).
--
-- CountA2ARunsForIssueSince asks "how many A2A-triggered runs on this issue
-- since T". The partial predicate keeps the index to the A2A subset, which is
-- a rounding error next to the whole queue, so the budget read costs nothing
-- on the ordinary human-triggered traffic that fills this table.
--
-- CONCURRENTLY and a single statement per repo policy: PostgreSQL refuses a
-- concurrent build inside a transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_issue_a2a_created ON agent_task_queue (issue_id, created_at) WHERE a2a_depth > 0;
