-- A person counts one view per note and per UTC day.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_note_usage_member_day
    ON workspace_note_usage (note_id, actor_id, kind, day) WHERE task_id IS NULL;
