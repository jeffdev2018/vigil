ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS permission_profile_id;
ALTER TABLE agent DROP COLUMN IF EXISTS permission_profile_id;
DROP TABLE IF EXISTS agent_permission_profile;
