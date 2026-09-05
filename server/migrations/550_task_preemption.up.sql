-- Preemption (K41): an urgent issue may suspend the lowest-priority run of
-- a saturated agent at its next safe boundary (the K19 pause), and that run
-- resumes on its own from its checkpoint once capacity frees, priority then
-- age. These columns keep the story on the suspended run.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS preempted_at TIMESTAMPTZ;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS preempted_by_task_id UUID;
