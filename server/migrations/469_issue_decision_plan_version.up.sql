-- Plan Gate (K11): a Decision Card that asks for a plan's approval carries
-- the plan version it is about, so answering "approve" materializes that one.
ALTER TABLE issue_decision ADD COLUMN IF NOT EXISTS plan_version INTEGER;
