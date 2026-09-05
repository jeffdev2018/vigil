-- Decision SLA (K35): a card asked under a workspace policy carries a
-- deadline; past it, the escalation job notifies the substitute, then the
-- workspace leads, and records how far it went.
ALTER TABLE issue_decision ADD COLUMN IF NOT EXISTS sla_deadline_at TIMESTAMPTZ;
ALTER TABLE issue_decision ADD COLUMN IF NOT EXISTS escalation_level INTEGER NOT NULL DEFAULT 0;
ALTER TABLE issue_decision ADD COLUMN IF NOT EXISTS escalated_at TIMESTAMPTZ;
