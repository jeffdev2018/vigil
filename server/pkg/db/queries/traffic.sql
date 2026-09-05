-- Traffic control (K18).

-- name: AppendTaskTouchedPaths :one
UPDATE agent_task_queue
SET touched_paths = (
    SELECT COALESCE(jsonb_agg(DISTINCT x), '[]'::jsonb)
    FROM jsonb_array_elements(COALESCE(touched_paths, '[]'::jsonb) || sqlc.arg(paths)::jsonb) AS x
)
WHERE id = $1
RETURNING *;

-- name: ListActiveTasksTouchingPaths :many
-- Other runs of the workspace still working that edit any of these paths.
SELECT t.* FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = $1
  AND t.id <> $2
  AND t.status IN ('running', 'dispatched', 'paused')
  AND t.touched_paths IS NOT NULL
  AND t.touched_paths ?| sqlc.arg(paths)::text[];

-- name: UpdateAgentRuntimeDirtyCheckouts :exec
UPDATE agent_runtime
SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('dirty_checkouts', sqlc.arg(dirty)::jsonb, 'dirty_checkouts_at', to_jsonb(now())),
    updated_at = now()
WHERE id = $1;

-- name: CreateTrafficConflict :one
INSERT INTO traffic_conflict (id, workspace_id, issue_id, task_id, kind, paths, other_task_id, handoff_packet_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: HasActiveTrafficConflict :one
SELECT EXISTS (
    SELECT 1 FROM traffic_conflict
    WHERE task_id = $1 AND kind = $2 AND other_task_id IS NOT DISTINCT FROM $3 AND status = 'active'
);

-- name: ListTrafficConflictsForIssue :many
SELECT * FROM traffic_conflict WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 50;

-- name: GetTrafficConflict :one
SELECT * FROM traffic_conflict WHERE id = $1;

-- name: SetTrafficConflictStatus :one
UPDATE traffic_conflict SET status = $2, resolved_at = now() WHERE id = $1 AND status = 'active' RETURNING *;

-- name: ResolveTrafficConflictsForFinishedRuns :exec
UPDATE traffic_conflict c SET status = 'resolved', resolved_at = now()
FROM agent_task_queue t
WHERE c.task_id = t.id AND c.issue_id = $1 AND c.status = 'active'
  AND t.status NOT IN ('running', 'dispatched', 'paused', 'queued', 'deferred', 'waiting_local_directory');

-- name: PurgeWorkspaceTrafficConflicts :exec
DELETE FROM traffic_conflict WHERE workspace_id = $1;
