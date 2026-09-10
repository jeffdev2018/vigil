-- Agent consult (JEF-12): persistence for POST /api/consult and its reads.
-- Rows are attributed to BOTH the workspace and the calling agent so spend
-- rollups can group by either without a join.

-- name: CreateAgentConsult :one
-- The pending row is written BEFORE the LLM call so a consult is recorded even
-- when the process dies mid-generation; FinalizeAgentConsultAnswer /
-- FinalizeAgentConsultFailure close it out.
INSERT INTO agent_consult (workspace_id, task_id, agent_id, model, question)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: InsertRefusedAgentConsult :one
-- A consult refused before any LLM call (budget exhausted, LLM layer disabled)
-- is still persisted, born in its terminal 'refused' state with the
-- machine-readable reason.
INSERT INTO agent_consult (workspace_id, task_id, agent_id, model, question, state, refusal_reason, finalized_at)
VALUES ($1, $2, $3, $4, $5, 'refused', $6, now())
RETURNING *;

-- name: CountAgentConsultsForTaskToday :one
-- The consult budget counter: rows this task already booked on the current
-- UTC day, refused ones included (a refused consult still asked). Counted
-- BEFORE the insert; the check is deliberately lightweight — a rare
-- count-then-insert race may admit one extra consult over the limit, which an
-- advisory-lock-free counter accepts by design.
SELECT COUNT(*)::bigint AS consult_count
FROM agent_consult
WHERE task_id = $1
  AND created_at >= date_trunc('day', now());

-- name: FinalizeAgentConsultAnswer :one
-- Only a still-pending row transitions, so a double finalize cannot resurrect
-- or overwrite a terminal state. Token counts and cost ride along; NULLs mean
-- the upstream did not report usage or the model has no known rate.
UPDATE agent_consult
SET state = 'answered',
    answer = $2,
    input_tokens = $3,
    output_tokens = $4,
    cost_usd_ticks = $5,
    finalized_at = now()
WHERE id = $1 AND state = 'pending'
RETURNING *;

-- name: FinalizeAgentConsultFailure :exec
-- Same pending-only guard. The (truncated) LLM error rides in refusal_reason:
-- it is why this consult produced no answer.
UPDATE agent_consult
SET state = 'failed',
    refusal_reason = $2,
    finalized_at = now()
WHERE id = $1 AND state = 'pending';

-- name: GetAgentConsult :one
SELECT * FROM agent_consult WHERE id = $1;

-- name: ListAgentConsultsByTask :many
-- Chronological: the execution log renders a run's consults in call order.
SELECT * FROM agent_consult
WHERE task_id = $1
ORDER BY created_at ASC, id ASC;
