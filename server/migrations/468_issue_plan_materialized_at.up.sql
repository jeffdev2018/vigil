-- Plan Gate (K11): a plan version records when its steps became sub-issues,
-- so an approval replayed twice cannot create them twice.
ALTER TABLE issue_plan ADD COLUMN IF NOT EXISTS materialized_at TIMESTAMPTZ;
COMMENT ON COLUMN issue_plan.materialized_at IS 'When this version''s steps were materialized as sub-issues; NULL until approved.';
