DROP TABLE IF EXISTS traffic_conflict;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS touched_paths;
