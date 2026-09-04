CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_log_entry_workspace_occurred ON audit_log_entry (workspace_id, occurred_at DESC, id DESC);
