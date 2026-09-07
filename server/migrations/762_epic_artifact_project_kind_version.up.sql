CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_epic_artifact_project_kind_version ON epic_artifact (project_id, kind, version);
