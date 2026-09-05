ALTER TABLE task_usage DROP COLUMN IF EXISTS model_key_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS model_key_id;
DROP TABLE IF EXISTS workspace_model_key;
