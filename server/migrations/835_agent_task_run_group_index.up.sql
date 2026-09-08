-- Attempts of one group, for the compare view and the winner/abandon paths.
--
-- Partial on IS NOT NULL: grouped rows are a rounding error next to the whole
-- queue, so this costs nothing on the ordinary traffic that fills the table.
--
-- CONCURRENTLY and a single statement per repository policy: PostgreSQL refuses
-- a concurrent build inside a transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_run_group ON agent_task_queue (run_group_id) WHERE run_group_id IS NOT NULL;
