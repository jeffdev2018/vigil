ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS routing,
    DROP COLUMN IF EXISTS task_class;
