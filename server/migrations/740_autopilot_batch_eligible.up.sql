-- Off-peak batch lane (K45). An autopilot marked batch_eligible declares that
-- its scheduled work is not urgent: when it fires inside the workspace's
-- off-peak window the run is stamped with the batch dispatch lane and yields
-- to every synchronous task of the same agent/runtime.
-- Default false, so no existing autopilot changes behaviour.
ALTER TABLE autopilot ADD COLUMN IF NOT EXISTS batch_eligible BOOLEAN NOT NULL DEFAULT false;
