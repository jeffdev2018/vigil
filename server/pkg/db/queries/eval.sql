-- Eval Lab (K24).

-- name: CreateEvalCase :one
INSERT INTO eval_case (id, workspace_id, source_issue_id, source_issue_number, title, description, criteria, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: ListEvalCases :many
SELECT * FROM eval_case WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 500;

-- name: GetEvalCasesByIDs :many
SELECT * FROM eval_case WHERE workspace_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[]);

-- name: CreateEvalSuite :one
INSERT INTO eval_suite (id, workspace_id, name, case_ids, created_by)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListEvalSuites :many
SELECT * FROM eval_suite WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 500;

-- name: GetEvalSuite :one
SELECT * FROM eval_suite WHERE id = $1;

-- name: CreateEvalRun :one
INSERT INTO eval_run (id, workspace_id, suite_id, agent_id, agent_version_id, started_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetEvalRun :one
SELECT * FROM eval_run WHERE id = $1;

-- name: ListEvalRuns :many
SELECT * FROM eval_run WHERE workspace_id = $1 ORDER BY started_at DESC LIMIT 200;

-- name: HasRunningEvalRunForSuite :one
SELECT EXISTS (SELECT 1 FROM eval_run WHERE suite_id = $1 AND status = 'running');

-- name: CreateEvalRunCase :one
INSERT INTO eval_run_case (run_id, case_id, issue_id, task_id)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListEvalRunCases :many
SELECT rc.*, c.title AS case_title
FROM eval_run_case rc
LEFT JOIN eval_case c ON c.id = rc.case_id
WHERE rc.run_id = $1
ORDER BY c.created_at ASC;

-- name: GetEvalRunCaseByTask :one
-- The eval case a run belongs to: its own run, or the retry chain of it.
-- Carries the run's pinned version so the claim can override the agent's
-- current configuration without a second query.
SELECT rc.*, r.agent_version_id, r.agent_id, r.workspace_id
FROM eval_run_case rc
JOIN eval_run r ON r.id = rc.run_id
WHERE rc.task_id IN (sqlc.arg(task_id), sqlc.arg(root_task_id))
LIMIT 1;

-- name: SettleEvalRunCase :one
UPDATE eval_run_case SET
    status     = sqlc.arg(status),
    score      = sqlc.narg(score),
    detail     = sqlc.arg(detail),
    task_id    = sqlc.arg(task_id),
    settled_at = now()
WHERE run_id = sqlc.arg(run_id) AND case_id = sqlc.arg(case_id) AND status = 'pending'
RETURNING *;

-- name: CountPendingEvalRunCases :one
SELECT COUNT(*) FROM eval_run_case WHERE run_id = $1 AND status = 'pending';

-- name: FinishEvalRun :one
UPDATE eval_run SET status = sqlc.arg(status), score = sqlc.narg(score), completed_at = now()
WHERE id = sqlc.arg(id) AND status = 'running' RETURNING *;

-- name: PurgeWorkspaceEvalRunCases :exec
DELETE FROM eval_run_case WHERE run_id IN (SELECT id FROM eval_run WHERE workspace_id = $1);

-- name: PurgeWorkspaceEvalRuns :exec
DELETE FROM eval_run WHERE workspace_id = $1;

-- name: PurgeWorkspaceEvalSuites :exec
DELETE FROM eval_suite WHERE workspace_id = $1;

-- name: PurgeWorkspaceEvalCases :exec
DELETE FROM eval_case WHERE workspace_id = $1;
