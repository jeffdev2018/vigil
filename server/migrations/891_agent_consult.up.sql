-- Agent consult (JEF-12): one row per synchronous consult an agent makes from
-- inside a running task (POST /api/consult). The server persists the row first,
-- then calls the internal LLM directly — no daemon run, no chat session.
--
-- State machine: pending -> answered | failed | refused.
--   - refused: budget exhausted or LLM layer disabled; refusal_reason carries
--     the machine-readable reason and finalized_at is stamped at insert.
--   - failed: the LLM call errored; refusal_reason carries a truncated error.
--   - answered: answer holds the model's reply.
-- cost_usd_ticks / input_tokens / output_tokens stay NULL when the LLM client
-- did not report usage (NULL = not reported; never estimated here).
-- workspace_id + agent_id are both denormalized onto the row so spend is
-- attributable to the workspace AND the calling agent without a join.
CREATE TABLE agent_consult (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    task_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    model TEXT NOT NULL,
    question TEXT NOT NULL,
    answer TEXT,
    state TEXT NOT NULL DEFAULT 'pending',
    refusal_reason TEXT,
    input_tokens BIGINT,
    output_tokens BIGINT,
    cost_usd_ticks BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at TIMESTAMPTZ
);

-- NOT VALID so 892 can validate the constraint in its own migration, keeping
-- the convention that a fresh CHECK never locks the table for a scan.
ALTER TABLE agent_consult
    ADD CONSTRAINT agent_consult_state_check
    CHECK (state IN ('pending', 'answered', 'failed', 'refused')) NOT VALID;
