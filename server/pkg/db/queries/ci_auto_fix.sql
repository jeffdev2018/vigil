-- CI auto-fix (K49).

-- name: CreateCIAutoFixRun :one
INSERT INTO ci_auto_fix_run (id, workspace_id, provider, pull_request_id, head_sha, issue_id, source_task_id, attempt, budget_usd_ticks, manual)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (pull_request_id, head_sha) DO NOTHING
RETURNING *;

-- name: SetCIAutoFixRunTask :exec
UPDATE ci_auto_fix_run SET task_id = $2 WHERE id = $1;

-- name: CountCIAutoFixRunsForPullRequest :one
SELECT count(*)::int FROM ci_auto_fix_run WHERE pull_request_id = $1;

-- name: ListCIAutoFixRunsForIssue :many
SELECT r.*, t.status AS task_status FROM ci_auto_fix_run r
LEFT JOIN agent_task_queue t ON t.id = r.task_id
WHERE r.issue_id = $1 ORDER BY r.created_at DESC;

-- name: GetCIAutoFixRunForTask :one
SELECT * FROM ci_auto_fix_run WHERE task_id = $1;

-- name: GetLatestCIAutoFixRunForPullRequest :one
SELECT * FROM ci_auto_fix_run WHERE pull_request_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: ListVCSPullRequestsForHead :many
SELECT * FROM vcs_pull_request WHERE connection_id = $1 AND head_sha = $2 AND head_sha <> '';

-- name: ListFailedVCSCommitStatuses :many
SELECT context, target_url, description FROM vcs_commit_status WHERE connection_id = $1 AND sha = $2 AND state = 'failed' ORDER BY context;

-- name: ListFailedGitHubCheckRuns :many
SELECT name, details_url, conclusion FROM github_pull_request_check_run
WHERE pr_id = $1 AND head_sha = $2 AND conclusion IN ('failure', 'error', 'timed_out', 'action_required') ORDER BY ordinal;

-- name: GetVCSPullRequestByID :one
SELECT * FROM vcs_pull_request WHERE id = $1;

-- name: ListTaskBranchesForIssue :many
SELECT id, agent_id, branch_name, status FROM agent_task_queue WHERE issue_id = $1 AND branch_name IS NOT NULL AND branch_name <> '' ORDER BY created_at DESC LIMIT 20;

-- name: PurgeWorkspaceCIAutoFixRuns :exec
DELETE FROM ci_auto_fix_run WHERE workspace_id = $1;

-- name: CIAutoFixExhaustedNoteExists :one
SELECT EXISTS (
    SELECT 1 FROM inbox_item
    WHERE workspace_id = $1 AND type = 'ci_auto_fix_exhausted'
      AND details->>'pull_request_id' = sqlc.arg(pull_request_id)::text AND details->>'head_sha' = sqlc.arg(head_sha)::text
);

-- name: DeleteCIAutoFixRun :exec
DELETE FROM ci_auto_fix_run WHERE id = $1;
