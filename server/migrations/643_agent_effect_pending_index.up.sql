-- End of run: the pending effects of one task, and a decision's effects.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_effect_task_pending
    ON agent_effect (task_id, created_at)
    WHERE status = 'pending';
