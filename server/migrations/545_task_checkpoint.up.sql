-- Checkpoints (K20): a run's resume point is a position in its own
-- task_message log plus the runtime session it already keeps. The server
-- advances it at every safe boundary; an infrastructure interruption
-- retries from it (bounded), and the attempt count tells the story.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS last_checkpoint_seq BIGINT;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS checkpoint_attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS checkpointed_at TIMESTAMPTZ;
