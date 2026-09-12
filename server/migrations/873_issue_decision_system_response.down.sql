ALTER TABLE issue_decision DROP CONSTRAINT IF EXISTS issue_decision_responded_by_type_check;
ALTER TABLE issue_decision ADD CONSTRAINT issue_decision_responded_by_type_check
    CHECK (responded_by_type IN ('member', 'agent')) NOT VALID;
