-- "Sometime between 08:00 and 10:00": a schedule trigger may spread its
-- firing over a band. cron_expression marks the start of the band; the
-- scheduler shifts each occurrence by a deterministic per-occurrence offset
-- within window_minutes (0 = fire exactly at the cron time).
ALTER TABLE autopilot_trigger ADD COLUMN IF NOT EXISTS window_minutes INTEGER NOT NULL DEFAULT 0;
