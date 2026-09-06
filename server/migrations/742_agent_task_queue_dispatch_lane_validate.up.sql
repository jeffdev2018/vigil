-- Validate the CHECK 741 added NOT VALID. Every pre-existing row carries the
-- column default 'sync', so the scan cannot fail.
ALTER TABLE agent_task_queue VALIDATE CONSTRAINT agent_task_queue_dispatch_lane_check;
