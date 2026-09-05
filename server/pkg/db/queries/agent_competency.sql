-- Learned competency (K43).

-- name: BumpAgentDomainCompetency :one
INSERT INTO agent_domain_competency (id, workspace_id, agent_id, domain_key, success_count, total_count, duel_wins, duel_losses)
VALUES ($1, $2, $3, $4, GREATEST(0, sqlc.arg(success_delta)::int), GREATEST(0, sqlc.arg(total_delta)::int), GREATEST(0, sqlc.arg(wins_delta)::int), GREATEST(0, sqlc.arg(losses_delta)::int))
ON CONFLICT (workspace_id, agent_id, domain_key) DO UPDATE SET
    success_count = GREATEST(0, agent_domain_competency.success_count + sqlc.arg(success_delta)::int),
    total_count   = GREATEST(0, agent_domain_competency.total_count + sqlc.arg(total_delta)::int),
    duel_wins     = GREATEST(0, agent_domain_competency.duel_wins + sqlc.arg(wins_delta)::int),
    duel_losses   = GREATEST(0, agent_domain_competency.duel_losses + sqlc.arg(losses_delta)::int),
    updated_at    = now()
RETURNING *;

-- name: ListAgentDomainCompetency :many
SELECT * FROM agent_domain_competency WHERE workspace_id = $1 AND agent_id = $2 ORDER BY total_count + duel_wins + duel_losses DESC, domain_key;

-- name: ListDomainCompetency :many
SELECT c.*, a.name AS agent_name FROM agent_domain_competency c
JOIN agent a ON a.id = c.agent_id
WHERE c.workspace_id = $1 AND c.domain_key = $2 AND a.archived_at IS NULL
ORDER BY c.agent_id;

-- name: PurgeWorkspaceAgentDomainCompetency :exec
DELETE FROM agent_domain_competency WHERE workspace_id = $1;

-- What-if estimate (K44). The competency audit trail is the only per-issue
-- record of (agent, domain), so it defines the comparable set: the most
-- recent issues this agent worked in this domain, then that agent's own
-- completed runs on them with their duration and settled cost.
-- name: ListComparableRunStats :many
WITH comparable AS (
    SELECT DISTINCT ON (a.details->>'issue_id')
           (a.details->>'issue_id')::uuid AS issue_id,
           a.occurred_at
    FROM audit_log_entry a
    WHERE a.workspace_id = $1
      AND a.action = 'competency'
      AND a.entity_type = 'agent'
      AND a.entity_id = $2
      AND a.details->>'domain_key' = sqlc.arg(domain_key)::text
      AND a.details->>'issue_id' ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    ORDER BY a.details->>'issue_id', a.occurred_at DESC
), recent AS (
    SELECT issue_id FROM comparable ORDER BY occurred_at DESC LIMIT sqlc.arg(row_limit)
)
SELECT t.id,
       EXTRACT(EPOCH FROM (t.completed_at - t.started_at))::bigint AS duration_seconds,
       COALESCE((SELECT SUM(u.cost_usd_ticks) FROM task_usage u WHERE u.task_id = t.id), 0)::bigint AS cost_ticks
FROM agent_task_queue t
JOIN recent r ON r.issue_id = t.issue_id
WHERE t.agent_id = $2
  AND t.status = 'completed'
  AND t.started_at IS NOT NULL
  AND t.completed_at IS NOT NULL
ORDER BY t.completed_at DESC;
