-- Agent review by agent (JEF-238): per-project review policy — checklist,
-- pinned reviewer, done gate with a bounded rework loop.

-- name: GetProjectReviewConfig :one
SELECT * FROM project_review_config WHERE project_id = $1;

-- name: UpsertProjectReviewConfig :one
INSERT INTO project_review_config (project_id, workspace_id, checklist, reviewer_agent_id, gate_enabled, max_cycles)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id) DO UPDATE SET
    checklist         = EXCLUDED.checklist,
    reviewer_agent_id = EXCLUDED.reviewer_agent_id,
    gate_enabled      = EXCLUDED.gate_enabled,
    max_cycles        = EXCLUDED.max_cycles,
    updated_at        = now()
RETURNING *;

-- name: PurgeWorkspaceProjectReviewConfigs :exec
-- project_review_config carries no FK by repo rule; sweep it by workspace.
DELETE FROM project_review_config WHERE workspace_id = $1;
