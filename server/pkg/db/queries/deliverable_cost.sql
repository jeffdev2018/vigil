-- Cost per deliverable (K04). Cost comes from task_usage through the runs
-- attached to an issue; an issue closed without any run has no row here.

-- name: ListCompletedIssueCosts :many
SELECT i.id, i.completed_at,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    BOOL_OR(tu.cost_usd_ticks IS NULL) AS uncosted
FROM issue i
JOIN agent_task_queue atq ON atq.issue_id = i.id
JOIN task_usage tu ON tu.task_id = atq.id
WHERE i.workspace_id = $1
  AND i.completed_at >= $2 AND i.completed_at < $3
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
GROUP BY i.id, i.completed_at;

-- name: ListMergedPullRequestCosts :many
-- A pull request carries the full cost of every issue it is linked to.
WITH merged AS (
    SELECT pr.id, pr.merged_at, ipr.issue_id
    FROM github_pull_request pr
    JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
    WHERE pr.workspace_id = $1 AND pr.state = 'merged' AND pr.merged_at >= $2 AND pr.merged_at < $3
    UNION ALL
    SELECT pr.id, pr.merged_at, ipr.issue_id
    FROM vcs_pull_request pr
    JOIN issue_vcs_pull_request ipr ON ipr.pull_request_id = pr.id
    WHERE pr.workspace_id = $1 AND pr.state = 'merged' AND pr.merged_at >= $2 AND pr.merged_at < $3
)
SELECT m.id, m.merged_at,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
    BOOL_OR(tu.cost_usd_ticks IS NULL) AS uncosted
FROM merged m
JOIN issue i ON i.id = m.issue_id
JOIN agent_task_queue atq ON atq.issue_id = i.id
JOIN task_usage tu ON tu.task_id = atq.id
WHERE (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
GROUP BY m.id, m.merged_at;
