-- F02: run-level liveness. The daemon flushes task messages every 500 ms while
-- the CLI talks, so stamping the row on every messages / progress callback
-- (and on claim / start) gives a per-run heartbeat without a new endpoint.
-- The "unresponsive" verdict is derived from this column at read time and
-- never stored; a silent run keeps its status.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS last_activity_at TIMESTAMPTZ;

COMMENT ON COLUMN agent_task_queue.last_activity_at IS
    'Last proof of activity from the run (claim, start, task message or progress callback). NULL means none recorded since the column was added. Read for liveness display only; never consulted for authorization or transitions.';
