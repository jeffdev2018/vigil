CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_transition_rule_lookup ON issue_transition_rule (workspace_id, project_id, to_category) WHERE enabled;
