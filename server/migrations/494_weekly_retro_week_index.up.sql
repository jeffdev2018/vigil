CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_weekly_retro_workspace_week ON weekly_retro (workspace_id, week_start);
