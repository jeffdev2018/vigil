CREATE INDEX CONCURRENTLY idx_workspace_doctrine_report_ws_status ON workspace_doctrine_report (workspace_id, status, created_at DESC);
