-- name: CreateAgentEffect :one
INSERT INTO agent_effect (
    id, workspace_id, task_id, agent_id, issue_id, kind, target_type, target_id,
    before, after, reversible, status, payload
) VALUES (
    $1, $2, $3, $4, sqlc.narg('issue_id')::uuid, $5, $6, $7,
    sqlc.arg('before')::jsonb, sqlc.arg('after')::jsonb, $8,
    sqlc.arg('status'), sqlc.arg('payload')::jsonb
)
RETURNING *;

-- name: ListPendingAgentEffectsForTask :many
-- Oldest first: approval replays the run in the order it happened.
SELECT * FROM agent_effect
WHERE task_id = $1 AND status = 'pending'
ORDER BY created_at ASC, id ASC;

-- name: ListAgentEffectsForDecision :many
SELECT * FROM agent_effect
WHERE decision_id = $1
ORDER BY created_at ASC, id ASC;

-- name: SetAgentEffectsDecision :execrows
UPDATE agent_effect SET decision_id = $2
WHERE task_id = $1 AND status = 'pending' AND decision_id IS NULL;

-- name: SetAgentEffectStatus :one
UPDATE agent_effect SET status = $3, reverse_error = sqlc.narg('error')
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetAgentEffectMode :one
UPDATE agent SET effect_mode = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ListAgentEffectsForIssue :many
SELECT * FROM agent_effect
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 200;

-- name: ListAgentEffectsForTask :many
-- Newest first: reversal walks the run backwards.
SELECT * FROM agent_effect
WHERE workspace_id = $1 AND task_id = $2
ORDER BY created_at DESC, id DESC;

-- name: GetAgentEffect :one
SELECT * FROM agent_effect WHERE id = $1 AND workspace_id = $2;

-- name: MarkAgentEffectReversed :one
UPDATE agent_effect
SET reversed_at = now(), reversed_by_type = $3, reversed_by_id = $4, reverse_error = NULL
WHERE id = $1 AND workspace_id = $2 AND reversed_at IS NULL
RETURNING *;

-- name: SetAgentEffectReverseError :exec
UPDATE agent_effect SET reverse_error = $3
WHERE id = $1 AND workspace_id = $2;

-- name: CountAgentRunsReversedSince :one
-- Distinct runs, not rows: undoing one run of twelve effects counts once.
SELECT COUNT(DISTINCT task_id)::bigint FROM agent_effect
WHERE workspace_id = $1 AND agent_id = $2 AND reversed_at IS NOT NULL AND reversed_at >= sqlc.arg('since')::timestamptz;
