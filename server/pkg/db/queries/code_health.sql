-- Code health autopilot (K22). A scheduled read-only agent run reports
-- maintenance opportunities; each scan row is the run plus what came out of it.

-- name: CreateCodeHealthScan :one
INSERT INTO code_health_scan (id, workspace_id, project_id, agent_id, task_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRunningCodeHealthScan :one
-- The "one scan at a time per workspace" guard. Ordered so a workspace that
-- somehow holds two running rows still resolves to the newest.
SELECT * FROM code_health_scan
WHERE workspace_id = $1 AND status = 'running'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetCodeHealthScanByTask :one
SELECT * FROM code_health_scan WHERE task_id = $1;

-- name: FinishCodeHealthScan :one
UPDATE code_health_scan
SET status = sqlc.arg('status'),
    findings = sqlc.arg('findings'),
    issues_created = sqlc.arg('issues_created'),
    error = sqlc.arg('error'),
    completed_at = now()
WHERE id = sqlc.arg('id') AND status = 'running'
RETURNING *;

-- name: ListCodeHealthScans :many
SELECT * FROM code_health_scan
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT 50;

-- name: GetLastCodeHealthScanAt :one
-- Cron anchor: the last time this workspace was scanned. No row means the
-- schedule is anchored on when it was enabled instead.
SELECT MAX(created_at)::timestamptz AS last_scan_at FROM code_health_scan
WHERE workspace_id = $1;

-- name: GetCodeHealthHostIssue :one
-- The one housekeeping issue every scan run hangs off. Recognised by its
-- origin: a scan-created maintenance issue always carries origin_id.
SELECT * FROM issue
WHERE workspace_id = $1 AND origin_type = 'code_health' AND origin_id IS NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: ListRecentCodeHealthIssueTitles :many
-- Dedupe source: what this autopilot already opened and nobody has closed.
SELECT title FROM issue
WHERE workspace_id = $1 AND origin_type = 'code_health' AND origin_id IS NOT NULL
  AND created_at >= sqlc.arg('since') AND completed_at IS NULL;

-- name: PurgeWorkspaceCodeHealthScans :exec
DELETE FROM code_health_scan WHERE workspace_id = $1;
