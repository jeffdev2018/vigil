ALTER TABLE project ADD COLUMN memory_source_review JSONB;
ALTER TABLE project_memory_version ADD COLUMN source_review JSONB;
