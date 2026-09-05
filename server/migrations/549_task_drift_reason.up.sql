-- Drift detection (K40): why a run was stopped for going in circles —
-- the same tool call repeated, or the same file re-read without a write
-- in between. Thresholds live in workspace settings (settings.drift).
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS drift_reason TEXT CHECK (drift_reason IS NULL OR drift_reason IN ('repeated_action', 'file_reread_loop'));
