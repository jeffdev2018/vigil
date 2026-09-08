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

-- name: ListAgentRoiRows :many
-- ROI per agent (JEF-252): what each agent spent over the window against what
-- it delivered. Every branch is a separate CTE LEFT JOINed onto `agent`, so an
-- agent that burned cost without closing anything still gets a row — that is
-- the row a purchase decision needs to see. Archived agents are included: they
-- spent real money while they were alive.
WITH closed AS (
    SELECT i.assignee_id AS agent_id, COUNT(DISTINCT i.id)::bigint AS issues_closed
    FROM issue i
    WHERE i.workspace_id = @workspace_id
      AND i.assignee_type = 'agent' AND i.assignee_id IS NOT NULL
      AND i.completed_at >= @period_start AND i.completed_at < @period_end
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
    GROUP BY i.assignee_id
),
runs AS (
    -- Every run costs, whether it landed or not, so failed runs count too.
    -- Under a project filter a run with no issue has no project and drops out.
    SELECT atq.agent_id,
        COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
        COUNT(DISTINCT atq.id) FILTER (WHERE tu.cost_usd_ticks IS NULL)::bigint AS uncosted_runs
    FROM agent_task_queue atq
    JOIN agent ag ON ag.id = atq.agent_id AND ag.workspace_id = @workspace_id
    JOIN task_usage tu ON tu.task_id = atq.id
    LEFT JOIN issue i ON i.id = atq.issue_id
    WHERE atq.status IN ('completed', 'failed')
      AND atq.completed_at >= @period_start AND atq.completed_at < @period_end
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
    GROUP BY atq.agent_id
),
merged AS (
    SELECT i.assignee_id AS agent_id, COUNT(DISTINCT m.id)::bigint AS prs_merged
    FROM (
        SELECT pr.id, ipr.issue_id
        FROM github_pull_request pr
        JOIN issue_pull_request ipr ON ipr.pull_request_id = pr.id
        WHERE pr.workspace_id = @workspace_id AND pr.state = 'merged'
          AND pr.merged_at >= @period_start AND pr.merged_at < @period_end
        UNION ALL
        SELECT pr.id, ipr.issue_id
        FROM vcs_pull_request pr
        JOIN issue_vcs_pull_request ipr ON ipr.pull_request_id = pr.id
        WHERE pr.workspace_id = @workspace_id AND pr.state = 'merged'
          AND pr.merged_at >= @period_start AND pr.merged_at < @period_end
    ) m
    JOIN issue i ON i.id = m.issue_id
    WHERE i.assignee_type = 'agent' AND i.assignee_id IS NOT NULL
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
    GROUP BY i.assignee_id
)
SELECT a.id, a.name,
    COALESCE(rt.provider, '')::text AS provider,
    COALESCE(c.issues_closed, 0)::bigint AS issues_closed,
    COALESCE(m.prs_merged, 0)::bigint AS prs_merged,
    COALESCE(r.cost_usd_ticks, 0)::bigint AS cost_usd_ticks,
    COALESCE(r.uncosted_runs, 0)::bigint AS uncosted_runs
FROM agent a
LEFT JOIN agent_runtime rt ON rt.id = a.runtime_id
LEFT JOIN closed c ON c.agent_id = a.id
LEFT JOIN runs r ON r.agent_id = a.id
LEFT JOIN merged m ON m.agent_id = a.id
WHERE a.workspace_id = @workspace_id
  AND (c.agent_id IS NOT NULL OR r.agent_id IS NOT NULL OR m.agent_id IS NOT NULL);
