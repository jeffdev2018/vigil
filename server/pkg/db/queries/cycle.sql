-- Dated cycles (F29). Every statement carries workspace_id: a cycle id alone
-- is never enough to reach a row.

-- name: ListCycles :many
SELECT * FROM cycle
WHERE workspace_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
ORDER BY start_date DESC, created_at DESC;

-- name: GetCycleInWorkspace :one
SELECT * FROM cycle WHERE id = $1 AND workspace_id = $2;

-- name: CreateCycle :one
INSERT INTO cycle (
    workspace_id, project_id, name, description, start_date, end_date,
    human_capacity, agent_capacity, load_property_id, rollover, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateCycle :one
UPDATE cycle SET
    name = $3,
    description = $4,
    start_date = $5,
    end_date = $6,
    human_capacity = $7,
    agent_capacity = $8,
    load_property_id = $9,
    rollover = $10,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CloseCycle :one
-- Idempotent: a cycle already closed keeps its original closed_at, so the
-- rollover job cannot re-open a burndown by running twice.
UPDATE cycle SET closed_at = COALESCE(closed_at, now()), updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteCycle :exec
DELETE FROM cycle WHERE id = $1 AND workspace_id = $2;

-- name: DeleteCyclesByProject :exec
DELETE FROM cycle WHERE project_id = $1 AND workspace_id = $2;

-- name: DeleteCycleSnapshotsByCycle :exec
DELETE FROM cycle_snapshot WHERE cycle_id = $1 AND workspace_id = $2;

-- name: DeleteCycleSnapshotsByProject :exec
DELETE FROM cycle_snapshot cs
WHERE cs.workspace_id = $2
  AND cs.cycle_id IN (SELECT c.id FROM cycle c WHERE c.project_id = $1 AND c.workspace_id = $2);

-- name: ClearIssueCycleByProject :exec
UPDATE issue SET cycle_id = NULL WHERE project_id = $1 AND workspace_id = $2 AND cycle_id IS NOT NULL;

-- name: ClearIssueCycleByCycle :exec
UPDATE issue SET cycle_id = NULL WHERE cycle_id = $1 AND workspace_id = $2;

-- name: SetIssueCycle :exec
-- Mirrors SetIssueGoal: cycle membership is written on its own rather than
-- through UpdateIssue, so no existing UpdateIssueParams builder has to learn
-- to pre-fill a new bare narg (which would silently clear it).
UPDATE issue SET cycle_id = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2;

-- name: GetCycleStats :many
-- Counts and loads for a set of cycles, in one pass.
--
-- load_property_key is the workspace number property that carries an issue's
-- load, as text ('' = none, load unit is the issue count). A stored value that
-- is not a jsonb number counts as 0 rather than failing the whole query — a
-- property can be retyped under a cycle that is already running.
--
-- Human vs agent is the assignee side, not a second capacity pool: 'member' is
-- human, 'agent' and 'squad' are agent, anything else (including NULL) is
-- unassigned and reported on its own so the two bars never absorb it.
SELECT i.cycle_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE i.status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done_count,
       COALESCE(SUM(
         CASE WHEN sqlc.arg('load_property_key')::text = '' THEN 1
              WHEN jsonb_typeof(i.properties -> sqlc.arg('load_property_key')::text) = 'number'
                THEN (i.properties ->> sqlc.arg('load_property_key')::text)::numeric
              ELSE 0 END
       ), 0)::numeric AS total_load,
       COALESCE(SUM(
         CASE WHEN NOT (i.status = ANY(sqlc.arg('terminal_status_keys')::text[])) THEN 0
              WHEN sqlc.arg('load_property_key')::text = '' THEN 1
              WHEN jsonb_typeof(i.properties -> sqlc.arg('load_property_key')::text) = 'number'
                THEN (i.properties ->> sqlc.arg('load_property_key')::text)::numeric
              ELSE 0 END
       ), 0)::numeric AS done_load,
       COALESCE(SUM(
         CASE WHEN i.assignee_type IS DISTINCT FROM 'member' THEN 0
              WHEN sqlc.arg('load_property_key')::text = '' THEN 1
              WHEN jsonb_typeof(i.properties -> sqlc.arg('load_property_key')::text) = 'number'
                THEN (i.properties ->> sqlc.arg('load_property_key')::text)::numeric
              ELSE 0 END
       ), 0)::numeric AS human_load,
       COALESCE(SUM(
         CASE WHEN i.assignee_type IS NULL OR i.assignee_type NOT IN ('agent', 'squad') THEN 0
              WHEN sqlc.arg('load_property_key')::text = '' THEN 1
              WHEN jsonb_typeof(i.properties -> sqlc.arg('load_property_key')::text) = 'number'
                THEN (i.properties ->> sqlc.arg('load_property_key')::text)::numeric
              ELSE 0 END
       ), 0)::numeric AS agent_load,
       COALESCE(SUM(
         CASE WHEN i.assignee_type IS NOT NULL THEN 0
              WHEN sqlc.arg('load_property_key')::text = '' THEN 1
              ELSE 0 END
       ), 0)::numeric AS unassigned_load
FROM issue i
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND i.cycle_id = ANY(sqlc.arg('cycle_ids')::uuid[])
GROUP BY i.cycle_id;

-- name: ListCycleSnapshots :many
SELECT * FROM cycle_snapshot
WHERE cycle_id = $1 AND workspace_id = $2
ORDER BY snapshot_date ASC;

-- name: UpsertCycleSnapshot :exec
-- Idempotent by (cycle_id, snapshot_date): re-running the job on the same day
-- overwrites the day's row rather than adding a second one.
INSERT INTO cycle_snapshot (
    cycle_id, snapshot_date, workspace_id,
    total_count, done_count, total_load, done_load, human_load, agent_load
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (cycle_id, snapshot_date) DO UPDATE SET
    total_count = EXCLUDED.total_count,
    done_count = EXCLUDED.done_count,
    total_load = EXCLUDED.total_load,
    done_load = EXCLUDED.done_load,
    human_load = EXCLUDED.human_load,
    agent_load = EXCLUDED.agent_load,
    updated_at = now();

-- name: ListOpenCyclesForSnapshot :many
-- Every started, unclosed cycle across every workspace. The snapshot job is
-- global; it groups the rows by workspace to resolve terminal status keys.
SELECT * FROM cycle
WHERE closed_at IS NULL AND start_date <= $1
ORDER BY workspace_id, start_date;

-- name: ListCyclesDueForRollover :many
-- Ended, still open. rollover=false cycles are included so the job can close
-- them without moving anything — a cycle that ends stays ended either way.
SELECT * FROM cycle
WHERE closed_at IS NULL AND end_date < $1
ORDER BY workspace_id, end_date;

-- name: FindNextCycle :one
-- The cycle work rolls into: same project, starting after this one, nearest
-- first, not already closed.
SELECT * FROM cycle
WHERE workspace_id = $1 AND project_id = $2 AND id <> $3
  AND closed_at IS NULL AND start_date > $4
ORDER BY start_date ASC, created_at ASC
LIMIT 1;

-- name: ListUnfinishedCycleIssues :many
SELECT * FROM issue
WHERE workspace_id = $1 AND cycle_id = $2
  AND NOT (status = ANY(sqlc.arg('terminal_status_keys')::text[]));

-- name: GetGoalProjectProgress :many
-- Per linked project, the progress of the issues that count for a goal: the
-- project's issues, plus any issue that names the goal directly. Terminal keys
-- come from the workspace catalogue, so a custom done-category status counts.
SELECT pg.project_id,
       p.title AS project_title,
       count(i.id)::bigint AS total_count,
       count(i.id) FILTER (WHERE i.status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done_count
FROM project_goal pg
JOIN project p ON p.id = pg.project_id AND p.workspace_id = pg.workspace_id
LEFT JOIN issue i ON i.workspace_id = pg.workspace_id
  AND i.project_id = pg.project_id
  AND (i.goal_id IS NULL OR i.goal_id = pg.goal_id)
WHERE pg.workspace_id = sqlc.arg('workspace_id')::uuid
  AND pg.goal_id = sqlc.arg('goal_id')::uuid
GROUP BY pg.project_id, p.title
ORDER BY p.title ASC;

-- name: DeleteIssueViewsByCycleScopeOfProject :exec
-- Cycle-scoped saved views die with their project, like the project-scoped
-- ones: the cycles they point at are deleted in the same transaction.
DELETE FROM issue_view v
WHERE v.workspace_id = $1
  AND v.scope_type = 'cycle'
  AND v.scope_id IN (SELECT c.id FROM cycle c WHERE c.project_id = $2 AND c.workspace_id = $1);
