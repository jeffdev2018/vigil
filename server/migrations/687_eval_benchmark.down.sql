ALTER TABLE eval_run_case
    DROP COLUMN IF EXISTS duration_seconds,
    DROP COLUMN IF EXISTS cost_usd_ticks,
    DROP COLUMN IF EXISTS task_class;

ALTER TABLE eval_run
    DROP COLUMN IF EXISTS baseline_run_id,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS runtime_id,
    DROP COLUMN IF EXISTS benchmark;
