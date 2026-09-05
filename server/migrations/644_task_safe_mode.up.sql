-- Replay (K70): a run replayed in safe mode holds every Multica write for
-- approval regardless of its agent's effect mode.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS safe_mode BOOLEAN NOT NULL DEFAULT false;
