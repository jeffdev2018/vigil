-- Internal benchmark harness (JEF-276). An eval suite already replays its
-- cases against ONE agent version; a benchmark replays the SAME suite against
-- several (runtime, model) candidates at once so their policies can be
-- compared on identical work.
--
-- benchmark marks such a run, runtime_id/model pin the candidate it measures
-- (the replay tasks are stamped with that runtime and forced onto that model
-- at claim), and baseline_run_id names the run this one is a delta against.
ALTER TABLE eval_run
    ADD COLUMN benchmark      BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN runtime_id     UUID,
    ADD COLUMN model          TEXT NOT NULL DEFAULT '',
    ADD COLUMN baseline_run_id UUID;

-- A benchmark is read per task class, so each case carries the class it was
-- classified as plus what it actually spent. Cost/duration are nullable: a
-- provider that reported no price and a run that never started are not zero.
ALTER TABLE eval_run_case
    ADD COLUMN task_class       TEXT NOT NULL DEFAULT 'general',
    ADD COLUMN cost_usd_ticks   BIGINT,
    ADD COLUMN duration_seconds INT;
