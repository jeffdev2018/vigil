-- One postmortem per failed task. The generator upserts on this key so a
-- redelivered task:failed event (or a rerun of the pass) cannot duplicate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_postmortem_source_task
    ON postmortem (source_task_id);
