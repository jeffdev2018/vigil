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
