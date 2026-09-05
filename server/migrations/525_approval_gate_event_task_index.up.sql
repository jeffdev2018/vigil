CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_approval_gate_event_task ON approval_gate_event (task_id, created_at);
