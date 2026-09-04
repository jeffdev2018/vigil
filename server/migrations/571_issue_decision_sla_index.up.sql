CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_decision_sla_pending ON issue_decision (sla_deadline_at) WHERE response IS NULL AND sla_deadline_at IS NOT NULL AND escalation_level < 2;
