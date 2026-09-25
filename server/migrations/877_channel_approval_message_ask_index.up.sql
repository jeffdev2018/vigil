CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_approval_message_ask ON channel_approval_message (workspace_id, source, ask_id) WHERE settled = false;
