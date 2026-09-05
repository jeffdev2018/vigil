ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS preempted_by_task_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS preempted_at;
