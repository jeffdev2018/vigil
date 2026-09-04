ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS failover_history;
ALTER TABLE agent DROP COLUMN IF EXISTS runtime_pool_id;
DROP TABLE IF EXISTS runtime_pool;
