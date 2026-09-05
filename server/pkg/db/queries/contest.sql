-- Contest (K72).

-- name: CreateContest :one
INSERT INTO contest (
    id, workspace_id, project_id, issue_id, target_type, target_id, target_excerpt,
    author_agent_id, author_provider, challenger_kind, challenger_agent_id, challenger_provider, same_vendor,
    challenger_task_id, max_rounds, status, auto, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
RETURNING *;

-- name: GetContest :one
SELECT * FROM contest WHERE id = $1 AND workspace_id = $2;

-- name: GetContestByTask :one
SELECT * FROM contest
WHERE challenger_task_id = $1 OR answer_task_id = $1
ORDER BY updated_at DESC
LIMIT 1;

-- name: ListContestsForTarget :many
SELECT * FROM contest
WHERE workspace_id = $1 AND target_type = $2 AND target_id = $3
ORDER BY created_at DESC;

-- name: ListContestsForIssue :many
SELECT * FROM contest
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at DESC
LIMIT 50;

-- name: CountContestsSince :one
-- The daily quota is per project; contests without a project share one bucket.
SELECT count(*) FROM contest
WHERE workspace_id = $1
  AND created_at >= sqlc.arg('since')::timestamptz
  AND ((sqlc.narg('project_id')::uuid IS NULL AND project_id IS NULL) OR project_id = sqlc.narg('project_id')::uuid);

-- name: SetContestObjections :one
UPDATE contest SET objections = $2, nothing_to_contest = $3, status = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetContestAnswers :one
UPDATE contest SET answers = $2, status = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetContestChallengerTask :one
UPDATE contest SET challenger_task_id = $2, round = $3, status = 'running', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetContestAnswerTask :one
UPDATE contest SET answer_task_id = $2, status = 'answering', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetContestStatus :exec
UPDATE contest SET status = $2, updated_at = now() WHERE id = $1;

-- name: ConfirmContest :one
UPDATE contest SET status = 'confirmed', human_verdict = $3, verdict_note = $4, confirmed_by = $5, confirmed_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status <> 'confirmed'
RETURNING *;

-- name: ListContestFallbackCandidates :many
-- Any other user agent, least recently used as a challenger first, when no
-- agent of another provider exists (same vendor, another agent/model).
SELECT a.id, a.name, COALESCE(r.provider, '')::text AS provider,
       (SELECT MAX(c.created_at) FROM contest c WHERE c.challenger_agent_id = a.id) AS last_contest_at
FROM agent a
LEFT JOIN agent_runtime r ON r.id = a.runtime_id
WHERE a.workspace_id = $1 AND a.kind = 'user' AND a.archived_at IS NULL AND a.id <> sqlc.arg('author_agent_id')::uuid
ORDER BY last_contest_at NULLS FIRST, a.created_at;

-- name: AvgAgentRecentTaskCostTicks :one
-- The pre-launch cost figure: the mean of the agent's five latest run costs.
SELECT COALESCE(AVG(t.cost), 0)::bigint FROM (
    SELECT COALESCE(SUM(u.cost_usd_ticks), 0) AS cost
    FROM agent_task_queue q
    JOIN task_usage u ON u.task_id = q.id
    WHERE q.agent_id = $1 AND q.status = 'completed'
    GROUP BY q.id
    ORDER BY MAX(q.created_at) DESC
    LIMIT 5
) t;

-- name: GetTriageItemForContest :one
SELECT * FROM triage_item WHERE id = $1 AND workspace_id = $2;

-- name: GetIssuePlanForContest :one
SELECT * FROM issue_plan WHERE id = $1 AND workspace_id = $2;

-- name: PurgeWorkspaceContests :exec
DELETE FROM contest WHERE workspace_id = $1;
