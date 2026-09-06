-- Off-peak batch lane (K45). The lane the scheduler stamped on this task.
-- 'sync' is every task ever enqueued before this column and everything urgent
-- after it; 'batch' is off-peak autopilot work that claim ordering puts last.
-- A non-volatile DEFAULT keeps the ADD COLUMN a catalog change, and the CHECK
-- lands NOT VALID so no table scan happens here; 742 validates it separately.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS dispatch_lane TEXT NOT NULL DEFAULT 'sync';
ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS agent_task_queue_dispatch_lane_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_dispatch_lane_check
    CHECK (dispatch_lane IN ('sync', 'batch'))
    NOT VALID;
