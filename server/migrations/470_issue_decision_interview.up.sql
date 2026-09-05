-- Requirement Interview (K13): up to three Decision Cards asked together
-- before coding. They share a group, keep their order, and remember the
-- status the issue returns to once every question is answered.
ALTER TABLE issue_decision ADD COLUMN IF NOT EXISTS interview_group_id UUID;
ALTER TABLE issue_decision ADD COLUMN IF NOT EXISTS interview_position INTEGER;
ALTER TABLE issue_decision ADD COLUMN IF NOT EXISTS interview_resume_status TEXT;
