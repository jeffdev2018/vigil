CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_handoff_packet_issue ON handoff_packet (issue_id, created_at DESC);
