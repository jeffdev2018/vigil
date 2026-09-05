-- Triage rules (K62): business rules can now watch webhook deliveries and
-- carry an action (dismiss, or accept with overrides) instead of a refusal.
ALTER TABLE business_rule DROP CONSTRAINT IF EXISTS business_rule_attach_point_check;
ALTER TABLE business_rule ADD CONSTRAINT business_rule_attach_point_check
    CHECK (attach_point IN ('project_create', 'issue_submit_review', 'agent_run_dispatch', 'webhook_received'));
ALTER TABLE business_rule ADD COLUMN IF NOT EXISTS action_spec JSONB;
