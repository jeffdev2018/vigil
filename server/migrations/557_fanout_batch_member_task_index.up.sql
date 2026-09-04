CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_fanout_batch_member_task ON fanout_batch_member (fanout_batch_id, task_id);
