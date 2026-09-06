-- Backing index for the scan history (K22), which reads one workspace's scans
-- newest first, and for the "is a scan already running" guard. Own
-- single-statement migration so CONCURRENTLY runs outside an implicit
-- transaction (repo convention).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_code_health_scan_workspace
    ON code_health_scan (workspace_id, created_at DESC);
