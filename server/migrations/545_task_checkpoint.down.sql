ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS checkpointed_at;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS checkpoint_attempts;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS last_checkpoint_seq;
