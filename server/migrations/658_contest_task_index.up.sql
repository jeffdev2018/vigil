CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_contest_challenger_task
    ON contest (challenger_task_id) WHERE challenger_task_id IS NOT NULL;
