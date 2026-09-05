-- Standup and retro (K34).

-- name: ListStaleBlockedIssues :many
SELECT issue.* FROM issue
WHERE issue.workspace_id = $1
  AND (issue.status = 'blocked' OR issue.status IN (SELECT s.key FROM issue_status s WHERE s.workspace_id = $1 AND s.category = 'blocked'))
  AND issue.updated_at < $2
ORDER BY issue.updated_at ASC
LIMIT 100;

-- name: ListStalePendingDecisionIssues :many
SELECT DISTINCT ON (i.id) i.* FROM issue i
JOIN issue_decision d ON d.issue_id = i.id
WHERE i.workspace_id = $1 AND d.response IS NULL AND d.created_at < $2
ORDER BY i.id
LIMIT 100;

-- name: CountStandupQuestionsSince :one
SELECT COUNT(*) FROM inbox_item
WHERE workspace_id = $1 AND type = 'standup_question' AND issue_id = $2 AND recipient_id = $3 AND created_at >= $4;

-- name: ListWorkspaceRunsBetween :many
SELECT t.id, t.agent_id, t.issue_id, t.status, t.started_at, t.completed_at, t.created_at, t.error
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = $1 AND t.created_at >= $2 AND t.created_at < $3
ORDER BY t.created_at ASC
LIMIT 2000;

-- name: SumAgentScorecardsBetween :many
SELECT agent_id,
       SUM(runs_total)::bigint AS runs_total,
       SUM(runs_failed)::bigint AS runs_failed,
       SUM(runs_accepted)::bigint AS runs_accepted,
       SUM(runs_reopened)::bigint AS runs_reopened,
       SUM(runs_no_intervention)::bigint AS runs_no_intervention,
       SUM(cost_usd_ticks_total)::bigint AS cost_usd_ticks_total
FROM agent_scorecard_daily
WHERE workspace_id = $1 AND day >= $2 AND day < $3
GROUP BY agent_id;

-- name: ListWorkspaceAgentNames :many
SELECT id, name FROM agent WHERE workspace_id = $1;

-- name: UpsertWeeklyRetro :one
INSERT INTO weekly_retro (id, workspace_id, week_start, summary, narrative, generated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (workspace_id, week_start) DO UPDATE
SET summary = EXCLUDED.summary, narrative = EXCLUDED.narrative, generated_at = now()
RETURNING *;

-- name: GetWeeklyRetro :one
SELECT * FROM weekly_retro WHERE workspace_id = $1 AND week_start = $2;

-- name: GetLatestWeeklyRetro :one
SELECT * FROM weekly_retro WHERE workspace_id = $1 ORDER BY week_start DESC LIMIT 1;

-- name: PurgeWorkspaceWeeklyRetros :exec
DELETE FROM weekly_retro WHERE workspace_id = $1;
