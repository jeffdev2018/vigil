-- Backing index for the hot path: every label attach on an issue asks
-- "which links fire for this project in this workspace?". Own single-statement
-- migration so CONCURRENTLY runs outside an implicit transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_mirror_link_source
    ON project_mirror_link (workspace_id, source_project_id);
