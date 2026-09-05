-- Run confidence scoring (JEF-240). confidence is the JSONB audit record
-- written when a completed run has been self-scored by the assist-layer LLM:
-- score (0..1), rationale, model, the workspace threshold that applied, and
-- whether the run landed below it (which routes the issue to human review).
-- NULL for unscored runs (disabled LLM, non-completed status, review runs,
-- rows that predate the feature).
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS confidence JSONB;
