CREATE UNIQUE INDEX CONCURRENTLY project_memory_correction_identity
ON project_memory_version (project_id, (source_review->>'review_id'))
WHERE source_review IS NOT NULL;
