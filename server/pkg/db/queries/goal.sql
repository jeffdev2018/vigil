-- Goals with ancestry (K74).

-- name: ListGoals :many
SELECT * FROM goal WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: GetGoalInWorkspace :one
SELECT * FROM goal WHERE id = $1 AND workspace_id = $2;

-- name: CreateGoal :one
INSERT INTO goal (id, workspace_id, parent_goal_id, title, description, success_measure, due_date, owner_id, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateGoal :one
UPDATE goal SET
    parent_goal_id = $3,
    title = $4,
    description = $5,
    success_measure = $6,
    due_date = $7,
    owner_id = $8,
    status = $9,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteGoal :exec
DELETE FROM goal WHERE id = $1 AND workspace_id = $2;

-- name: CountChildGoals :one
SELECT count(*) FROM goal WHERE parent_goal_id = $1 AND workspace_id = $2;

-- name: ListProjectGoals :many
SELECT * FROM project_goal WHERE workspace_id = $1 ORDER BY created_at ASC;

-- name: ListProjectGoalsByProject :many
SELECT pg.* FROM project_goal pg WHERE pg.project_id = $1 AND pg.workspace_id = $2 ORDER BY pg.created_at ASC;

-- name: AddProjectGoal :exec
INSERT INTO project_goal (workspace_id, project_id, goal_id) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: DeleteProjectGoalsByProject :exec
DELETE FROM project_goal WHERE project_id = $1 AND workspace_id = $2;

-- name: DeleteProjectGoalsByGoal :exec
DELETE FROM project_goal WHERE goal_id = $1 AND workspace_id = $2;

-- name: SetIssueGoal :exec
UPDATE issue SET goal_id = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2;

-- name: ClearIssueGoal :exec
UPDATE issue SET goal_id = NULL WHERE goal_id = $1 AND workspace_id = $2;

-- name: GetGoalIssueStats :many
-- An issue counts for a goal when it names the goal or, without one of its
-- own, when its project is linked to the goal (inheritance).
SELECT g.id AS goal_id,
       count(i.id)::bigint AS total_count,
       count(i.id) FILTER (WHERE i.status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done_count
FROM goal g
LEFT JOIN issue i ON i.workspace_id = g.workspace_id
  AND (i.goal_id = g.id
       OR (i.goal_id IS NULL AND i.project_id IN (SELECT pg.project_id FROM project_goal pg WHERE pg.goal_id = g.id)))
WHERE g.workspace_id = sqlc.arg('workspace_id')::uuid
GROUP BY g.id;

-- name: ListGoalIssues :many
SELECT i.* FROM issue i
WHERE i.workspace_id = $1
  AND (i.goal_id = $2
       OR (i.goal_id IS NULL AND i.project_id IN (SELECT pg.project_id FROM project_goal pg WHERE pg.goal_id = $2)))
ORDER BY i.updated_at DESC
LIMIT 200;
