-- Agent-to-agent hop distance from the human at the head of the chain
-- (F19 / JEF-32).
--
-- 0 for a run a human triggered; parent.a2a_depth + 1 for a run an A2A message
-- triggered, the parent being resolved through comment.source_task_id — the
-- single hop attributionFromComment already walks.
--
-- STORED, not derived. The attribution chain is COPIED rather than linked
-- (migration 184 sets delegated_from_task_id from the parent's own value), so
-- after the fact there is no edge left to count. Depth has to be written at
-- enqueue time or it cannot be known at all.
--
-- NOT NULL DEFAULT 0 is safe on this table: a non-volatile default is stored in
-- the catalog since PG11, so no table rewrite. Every pre-existing run is
-- correctly 0 — none of them came from an A2A message, which did not exist.
--
-- Circuit breaker only. This is NOT an authorization signal and must never be
-- read as one: canInvokeAgent still judges every hop by the human originator.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS a2a_depth INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN agent_task_queue.a2a_depth IS
    'Agent-to-agent hop count from the human originator (F19). 0 = human-triggered. Circuit breaker only, never an authorization signal.';
