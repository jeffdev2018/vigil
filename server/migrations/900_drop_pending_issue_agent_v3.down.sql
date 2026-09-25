-- Restore v3 exactly as migration 837 built it.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_one_pending_task_per_issue_agent_v3
    ON agent_task_queue (issue_id, agent_id)
    WHERE run_group_id IS NULL
      AND (status IN ('queued', 'dispatched')
           OR (status = 'deferred' AND context->>'channel_issue_media_pending' = 'true'));
