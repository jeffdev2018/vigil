-- Agent duel (K39).

-- name: CreateAgentDuel :one
INSERT INTO agent_duel (id, workspace_id, issue_id, agent_a_id, agent_b_id, task_a_id, task_b_id, started_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: GetAgentDuel :one
SELECT * FROM agent_duel WHERE id = $1;

-- name: GetLatestAgentDuelForIssue :one
SELECT * FROM agent_duel WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: HasRunningAgentDuelForIssue :one
SELECT EXISTS (SELECT 1 FROM agent_duel WHERE issue_id = $1 AND status = 'running');

-- name: GetRunningAgentDuelForTask :one
-- The duel a run belongs to: its own run, or the retry chain of it.
SELECT * FROM agent_duel
WHERE status = 'running'
  AND (task_a_id IN (sqlc.arg(task_id), sqlc.arg(root_task_id)) OR task_b_id IN (sqlc.arg(task_id), sqlc.arg(root_task_id)))
LIMIT 1;

-- name: SettleAgentDuelSide :one
UPDATE agent_duel SET
    outcome_a       = CASE WHEN sqlc.arg(side)::text = 'a' THEN sqlc.arg(outcome)::text ELSE outcome_a END,
    final_task_a_id = CASE WHEN sqlc.arg(side)::text = 'a' THEN sqlc.arg(final_task_id)::uuid ELSE final_task_a_id END,
    outcome_b       = CASE WHEN sqlc.arg(side)::text = 'b' THEN sqlc.arg(outcome)::text ELSE outcome_b END,
    final_task_b_id = CASE WHEN sqlc.arg(side)::text = 'b' THEN sqlc.arg(final_task_id)::uuid ELSE final_task_b_id END
WHERE id = sqlc.arg(id) AND status = 'running' RETURNING *;

-- name: SetAgentDuelVerdict :one
UPDATE agent_duel SET status = $2, verdict = $3, arbiter_error = $4, settled_at = now()
WHERE id = $1 AND status = 'running' RETURNING *;

-- name: ConfirmAgentDuel :one
UPDATE agent_duel SET status = 'confirmed', winner = $2, confirmed_by = $3, confirmed_at = now()
WHERE id = $1 AND status = 'verdict_ready' RETURNING *;

-- name: PurgeWorkspaceAgentDuels :exec
DELETE FROM agent_duel WHERE workspace_id = $1;
