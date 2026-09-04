CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_log_entry_chain ON audit_log_entry (workspace_id, chain_seq);
