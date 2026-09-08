ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS run_group_id,
    DROP COLUMN IF EXISTS model_override,
    DROP COLUMN IF EXISTS diff_stat,
    DROP COLUMN IF EXISTS diff_unified;
