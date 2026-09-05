CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_contest_answer_task
    ON contest (answer_task_id) WHERE answer_task_id IS NOT NULL;
