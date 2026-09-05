-- Task watchdog (K73).

-- name: UpsertIssueWatchdog :one
INSERT INTO issue_watchdog (id, workspace_id, issue_id, agent_id, owner_id, instructions, rest_minutes, enabled, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (issue_id) DO UPDATE SET
    agent_id = EXCLUDED.agent_id, owner_id = EXCLUDED.owner_id, instructions = EXCLUDED.instructions,
    rest_minutes = EXCLUDED.rest_minutes, enabled = EXCLUDED.enabled, updated_at = now()
RETURNING *;

-- name: GetIssueWatchdog :one
SELECT * FROM issue_watchdog WHERE issue_id = $1 AND workspace_id = $2;

-- name: GetWatchdog :one
SELECT * FROM issue_watchdog WHERE id = $1 AND workspace_id = $2;

-- name: GetWatchdogByScanTask :one
SELECT * FROM issue_watchdog WHERE last_scan_task_id = $1;

-- name: DeleteIssueWatchdog :execrows
DELETE FROM issue_watchdog WHERE issue_id = $1 AND workspace_id = $2;

-- name: ListEnabledWatchdogs :many
SELECT * FROM issue_watchdog WHERE enabled ORDER BY created_at ASC LIMIT 1000;

-- name: SetWatchdogScan :exec
UPDATE issue_watchdog SET last_scan_task_id = $2, last_scanned_at = now(), updated_at = now() WHERE id = $1;

-- name: SetWatchdogMotionStreak :exec
UPDATE issue_watchdog SET motion_streak = $2, updated_at = now() WHERE id = $1;

-- name: CreateWatchdogVerdict :one
INSERT INTO watchdog_verdict (id, workspace_id, watchdog_id, issue_id, task_id, verdict, summary, findings, dropped, applied, decision_id, contract_revision)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, sqlc.narg('decision_id')::uuid, $11)
RETURNING *;

-- name: GetWatchdogVerdict :one
SELECT * FROM watchdog_verdict WHERE id = $1 AND workspace_id = $2;

-- name: GetWatchdogVerdictByDecision :one
SELECT * FROM watchdog_verdict WHERE decision_id = $1;

-- name: GetWatchdogVerdictByTask :one
SELECT * FROM watchdog_verdict WHERE task_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: ListWatchdogVerdicts :many
SELECT * FROM watchdog_verdict WHERE watchdog_id = $1 ORDER BY created_at DESC LIMIT 50;

-- name: SetWatchdogVerdictReview :one
UPDATE watchdog_verdict SET human_review = $3, applied = $4 WHERE id = $1 AND workspace_id = $2 RETURNING *;

-- name: WatchdogReviewStats :one
-- Over the agent's last 30 reviewed verdicts: how many a human confirmed.
SELECT COUNT(*)::bigint AS reviewed, COUNT(*) FILTER (WHERE human_review = 'confirmed')::bigint AS confirmed
FROM (
    SELECT v.human_review FROM watchdog_verdict v
    JOIN issue_watchdog w ON w.id = v.watchdog_id
    WHERE w.agent_id = $1 AND v.human_review <> 'pending'
    ORDER BY v.created_at DESC LIMIT 30
) recent;

-- name: CountWatchdogReopensSince :one
SELECT COUNT(*)::bigint FROM watchdog_verdict
WHERE workspace_id = $1 AND created_at >= sqlc.arg('since')::timestamptz AND (applied->>'reopened')::int > 0;

-- name: DeleteWorkspaceWatchdogs :exec
DELETE FROM issue_watchdog WHERE workspace_id = $1;

-- name: DeleteWorkspaceWatchdogVerdicts :exec
DELETE FROM watchdog_verdict WHERE workspace_id = $1;

-- name: SetWatchdogVerdictDecision :one
UPDATE watchdog_verdict SET decision_id = $2 WHERE id = $1 RETURNING *;

-- name: ListWatchdogVerdictsForTask :many
SELECT * FROM watchdog_verdict WHERE task_id = $1 ORDER BY created_at ASC;
