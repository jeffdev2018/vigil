ALTER TABLE business_rule DROP COLUMN IF EXISTS action_spec;
ALTER TABLE business_rule DROP CONSTRAINT IF EXISTS business_rule_attach_point_check;
ALTER TABLE business_rule ADD CONSTRAINT business_rule_attach_point_check
    CHECK (attach_point IN ('project_create', 'issue_submit_review', 'agent_run_dispatch'));
