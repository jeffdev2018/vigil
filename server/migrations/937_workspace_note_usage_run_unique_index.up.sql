-- A run counts a note once per kind: a re-claim or a repeated search of the
-- same run inserts nothing (ON CONFLICT ... WHERE task_id IS NOT NULL).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_usage_run
    ON workspace_note_usage (task_id, note_id, kind) WHERE task_id IS NOT NULL;
