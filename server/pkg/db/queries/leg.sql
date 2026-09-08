-- Per-leg accounting (JEF-274): a workflow is the primary run plus every
-- secondary leg (review, revision, retry, fallback, escalation, ...) that
-- points back at it through workflow_root_task_id.

-- name: SetTaskLeg :one
-- Stamps a freshly created run with its role and the workflow it belongs to.
-- A NULL workflow_root_task_id leaves the leg its own root, which is what a
-- producer without a parent run (duel candidate, fan-out synthesis, campaign
-- shard) records.
UPDATE agent_task_queue
SET leg_role = $2, workflow_root_task_id = $3
WHERE id = $1
RETURNING *;

-- name: ListWorkflowLegs :many
-- Every leg of one workflow, addressed by its root: the root run itself plus
-- the legs pointing at it. Each leg carries the identity needed to read it
-- (agent, runtime) and its own spend, so the caller can total the workflow
-- without a second pass over task_usage.
--
-- provider/model name the leg's DOMINANT usage row (highest cost, then
-- tokens), the same one-attribution-per-run rule GetRoutingStats applies, so a
-- multi-model leg reads as one provider/model while its token and cost sums
-- still cover every model it used. A leg with no usage row at all comes back
-- with empty provider/model and zero spend rather than being dropped: an
-- unpriced attempt is still a leg of the workflow.
SELECT
    t.id,
    t.leg_role,
    t.status,
    t.agent_id,
    COALESCE(a.name, '') AS agent_name,
    t.runtime_id,
    COALESCE(r.name, '') AS runtime_name,
    t.created_at,
    t.completed_at,
    COALESCE(EXTRACT(EPOCH FROM (t.completed_at - t.started_at)), 0)::float8 AS duration_seconds,
    COALESCE(tu.provider, COALESCE(r.provider, ''))::text AS provider,
    COALESCE(tu.model, '')::text AS model,
    COALESCE(tu.input_tokens, 0)::bigint AS input_tokens,
    COALESCE(tu.output_tokens, 0)::bigint AS output_tokens,
    COALESCE(tu.cost_usd_ticks, 0)::bigint AS cost_usd_ticks
FROM agent_task_queue t
LEFT JOIN agent a ON a.id = t.agent_id
LEFT JOIN agent_runtime r ON r.id = t.runtime_id
LEFT JOIN LATERAL (
    SELECT
        ((array_agg(u.provider ORDER BY u.cost_usd_ticks DESC NULLS LAST,
            (u.input_tokens + u.output_tokens) DESC, u.id))[1])::text AS provider,
        ((array_agg(u.model ORDER BY u.cost_usd_ticks DESC NULLS LAST,
            (u.input_tokens + u.output_tokens) DESC, u.id))[1])::text AS model,
        SUM(u.input_tokens)::bigint AS input_tokens,
        SUM(u.output_tokens)::bigint AS output_tokens,
        SUM(u.cost_usd_ticks)::bigint AS cost_usd_ticks
    FROM task_usage u
    WHERE u.task_id = t.id
) tu ON TRUE
WHERE t.id = $1 OR t.workflow_root_task_id = $1
ORDER BY t.created_at, t.id;
