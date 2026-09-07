CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_epic_artifact_project_state ON epic_artifact (project_id, state);
