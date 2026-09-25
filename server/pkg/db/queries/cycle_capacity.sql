-- JEF-246: per-actor cycle capacity and velocity. Every statement carries
-- workspace_id: a cycle id alone is never enough to reach a row.

-- name: ListCycleActorCapacities :many
-- The cycle's declared capacities with the actor's display name resolved.
-- Members sort before agents, then by name — the order the contract freezes.
SELECT cac.cycle_id, cac.actor_type, cac.actor_id, cac.points,
       COALESCE(u.name, a.name, '') AS name
FROM cycle_actor_capacity cac
LEFT JOIN member m ON m.workspace_id = cac.workspace_id
  AND cac.actor_type = 'member' AND m.user_id = cac.actor_id
LEFT JOIN "user" u ON u.id = m.user_id
LEFT JOIN agent a ON a.workspace_id = cac.workspace_id
  AND cac.actor_type = 'agent' AND a.id = cac.actor_id
WHERE cac.cycle_id = $1 AND cac.workspace_id = $2
ORDER BY CASE WHEN cac.actor_type = 'member' THEN 0 ELSE 1 END, COALESCE(u.name, a.name, '') ASC, cac.actor_id ASC;

-- name: InsertCycleActorCapacity :one
INSERT INTO cycle_actor_capacity (cycle_id, workspace_id, actor_type, actor_id, points)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteCycleActorCapacitiesByCycle :exec
DELETE FROM cycle_actor_capacity WHERE cycle_id = $1 AND workspace_id = $2;

-- name: DeleteCycleActorCapacitiesByProject :exec
-- Keyed by cycle, so a project delete goes through the cycle set it is about
-- to remove — same shape as DeleteCycleSnapshotsByProject.
DELETE FROM cycle_actor_capacity cac
WHERE cac.workspace_id = $2
  AND cac.cycle_id IN (SELECT c.id FROM cycle c WHERE c.project_id = $1 AND c.workspace_id = $2);

-- name: ListCycleActorNames :many
-- Display names for a mixed set of capacity/velocity actors. Member actor ids
-- are user ids (the identity issue.assignee_id carries), agent actor ids are
-- agent rows.
SELECT 'member'::text AS actor_type, m.user_id AS actor_id, u.name
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = sqlc.arg('workspace_id')::uuid
  AND m.user_id = ANY(sqlc.arg('member_ids')::uuid[])
UNION ALL
SELECT 'agent'::text, a.id, a.name
FROM agent a
WHERE a.workspace_id = sqlc.arg('workspace_id')::uuid
  AND a.id = ANY(sqlc.arg('agent_ids')::uuid[]);

-- name: GetCycleVelocityActors :many
-- Done load of one cycle, per direct assignee. Done is the status CATALOGUE's
-- done category (the caller passes its keys), not a literal status name, so a
-- custom done status counts. load_property_key is the cycle's number load
-- property as text ('' = the unit is the issue count). A stored value that is
-- not a jsonb number counts as 0 rather than failing the query — a property
-- can be retyped under a cycle that is already running.
--
-- Squad-assigned and unassigned issues are NOT here by design: they have no
-- single actor to credit, so they roll up into GetCycleOtherDonePoints
-- instead of being smeared across people who did not do them.
SELECT i.assignee_type AS actor_type,
       i.assignee_id AS actor_id,
       count(*)::bigint AS done_count,
       COALESCE(SUM(
         CASE WHEN sqlc.arg('load_property_key')::text = '' THEN 1
              WHEN jsonb_typeof(i.properties -> sqlc.arg('load_property_key')::text) = 'number'
                THEN (i.properties ->> sqlc.arg('load_property_key')::text)::numeric
              ELSE 0 END
       ), 0)::numeric AS done_points
FROM issue i
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND i.cycle_id = sqlc.arg('cycle_id')::uuid
  AND i.status = ANY(sqlc.arg('terminal_status_keys')::text[])
  AND i.assignee_type IN ('member', 'agent')
  AND i.assignee_id IS NOT NULL
GROUP BY i.assignee_type, i.assignee_id;

-- name: GetCycleOtherDonePoints :one
-- The complement of GetCycleVelocityActors: done load with no direct
-- member/agent assignee (squad-assigned or unassigned).
SELECT COALESCE(SUM(
         CASE WHEN sqlc.arg('load_property_key')::text = '' THEN 1
              WHEN jsonb_typeof(i.properties -> sqlc.arg('load_property_key')::text) = 'number'
                THEN (i.properties ->> sqlc.arg('load_property_key')::text)::numeric
              ELSE 0 END
       ), 0)::numeric AS done_points
FROM issue i
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND i.cycle_id = sqlc.arg('cycle_id')::uuid
  AND i.status = ANY(sqlc.arg('terminal_status_keys')::text[])
  AND (i.assignee_type IS NULL OR i.assignee_id IS NULL OR i.assignee_type NOT IN ('member', 'agent'));

-- name: ListCycleVelocityHistory :many
-- The cycles a velocity report compares against: the OTHER cycles of the same
-- project, most recent first. The NULL project_id branch is the contract's
-- project-less fallback; cycle.project_id is NOT NULL today, so it is
-- defensive, not live.
SELECT * FROM cycle
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND id <> sqlc.arg('id')::uuid
  AND ((sqlc.narg('project_id')::uuid IS NULL AND project_id IS NULL)
       OR (sqlc.narg('project_id')::uuid IS NOT NULL AND project_id = sqlc.narg('project_id')::uuid))
ORDER BY start_date DESC, created_at DESC
LIMIT 12;
