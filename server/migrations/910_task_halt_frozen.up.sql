-- JEF-257: the workspace halt freezes in-flight runs through the K19 pause
-- machinery instead of letting them run on. halt_frozen_at marks a run the
-- halt froze (as opposed to a run a human paused, or one suspended by K41
-- preemption) so lifting the halt resumes exactly those runs and nothing
-- else. Nullable: no CHECK, no backfill.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS halt_frozen_at TIMESTAMPTZ;
