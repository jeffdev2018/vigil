-- Per-task routing metadata (JEF-237). task_class is a deterministic label
-- (bugfix, feature, refactor, docs, tests, chore, general) derived from the
-- issue title and labels at enqueue time; it is stamped on EVERY task so the
-- routing statistics can segment success / cost / duration per class,
-- including for agents still in fixed mode. routing is the JSONB audit trace
-- written when the runtime router (agent.runtime_routing = 'auto') decided
-- this task's (runtime, model): mode, chosen pair, reason, and the full
-- scored candidate list. NULL for fixed-mode tasks.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS task_class TEXT NOT NULL DEFAULT 'general',
    ADD COLUMN IF NOT EXISTS routing JSONB;
