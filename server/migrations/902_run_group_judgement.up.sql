-- The LLM judge's verdict on a run group (JEF-234 follow-up).
--
-- One JSONB blob on the group row, NULL until a human asks for a judgement.
-- The shape is the API's judgement object: status ('answered' | 'failed'),
-- winner_task_id, justification, per-attempt scores, the model that judged,
-- judged_at, and the judge call's own cost in ticks. A failed judgement is
-- stored too — a malformed or self-contradictory LLM reply is a fact about
-- the judging attempt, and re-judging simply overwrites it.
ALTER TABLE run_group ADD COLUMN IF NOT EXISTS judgement JSONB;

COMMENT ON COLUMN run_group.judgement IS
    'LLM judge verdict (JEF-234): {status, winner_task_id, justification, scores[], model, judged_at, cost_usd_ticks}. NULL until judged.';
