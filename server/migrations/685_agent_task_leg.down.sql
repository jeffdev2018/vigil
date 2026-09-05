ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS leg_role,
    DROP COLUMN IF EXISTS workflow_root_task_id;
