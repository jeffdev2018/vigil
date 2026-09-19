-- Inline approvals (OS plan, chantier 3): a Decision Card behind an approval
-- gate that timed out is answered by the server itself, so the card leaves
-- every inbox and timeline instead of staying "pending" forever. 'system' is
-- the responder for that; responded_by_id stays NULL.
ALTER TABLE issue_decision DROP CONSTRAINT IF EXISTS issue_decision_responded_by_type_check;
ALTER TABLE issue_decision ADD CONSTRAINT issue_decision_responded_by_type_check
    CHECK (responded_by_type IN ('member', 'agent', 'system')) NOT VALID;
