ALTER TABLE issue_decision DROP COLUMN IF EXISTS escalated_at;
ALTER TABLE issue_decision DROP COLUMN IF EXISTS escalation_level;
ALTER TABLE issue_decision DROP COLUMN IF EXISTS sla_deadline_at;
