CREATE INDEX CONCURRENTLY idx_brain_capture_ws_status ON brain_capture (workspace_id, status, created_at DESC, id DESC);
