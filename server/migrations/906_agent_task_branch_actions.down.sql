ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS discarded_at;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS promote_pr_url;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS promoted_at;
