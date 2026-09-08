ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS agent_task_queue_dispatch_lane_check;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS dispatch_lane;
