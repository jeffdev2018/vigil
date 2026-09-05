-- Trust Dial (K26).

-- name: SetAgentTrustMode :one
UPDATE agent SET trust_mode = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: CreateTrustModeChange :one
INSERT INTO trust_mode_change (id, workspace_id, agent_id, from_mode, to_mode, reason, triggered_by_type, triggered_by_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListTrustModeChanges :many
SELECT * FROM trust_mode_change WHERE agent_id = $1 ORDER BY created_at DESC, id DESC LIMIT 50;

-- name: HasMaterializedIssuePlan :one
SELECT EXISTS (SELECT 1 FROM issue_plan WHERE issue_id = $1 AND materialized_at IS NOT NULL);

-- name: ListAgentsForTrustSuggestions :many
SELECT * FROM agent WHERE status <> 'archived' AND trust_mode <> 'autonomous' ORDER BY workspace_id, created_at;

-- name: CountTrustSuggestionNoticesSince :one
SELECT COUNT(*) FROM inbox_item
WHERE workspace_id = $1 AND type = 'trust_promotion_suggested' AND details->>'agent_id' = sqlc.arg(agent_id)::text AND created_at >= sqlc.arg(since);

-- name: PurgeWorkspaceTrustModeChanges :exec
DELETE FROM trust_mode_change WHERE workspace_id = $1;
