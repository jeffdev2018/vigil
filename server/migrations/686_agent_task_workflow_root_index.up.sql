-- Backing index for ListWorkflowLegs (JEF-274), which gathers a workflow from
-- any of its legs. Partial: only secondary legs carry a root, so the index
-- stays proportional to the workflows rather than to agent_task_queue — the
-- largest table in the database. Own single-statement migration so
-- CONCURRENTLY runs outside an implicit transaction (repo convention).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_workflow_root
    ON agent_task_queue (workflow_root_task_id)
    WHERE workflow_root_task_id IS NOT NULL;
