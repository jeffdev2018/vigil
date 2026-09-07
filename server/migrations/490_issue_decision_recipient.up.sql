CREATE INDEX CONCURRENTLY issue_decision_recipient ON issue_decision (workspace_id, recipient_id, created_at DESC, id DESC);
