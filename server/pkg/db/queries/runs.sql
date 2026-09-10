-- Fleet page (OS plan, chantier 4): every run of one workspace, filtered,
-- keyset-paginated, with what each is blocked on and what it cost.
-- agent_task_queue has no workspace_id: every read joins agent, and the
-- caller passes the agent ids the actor may see (private agents stay out).

-- name: ListWorkspaceRuns :many
SELECT atq.* FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND (sqlc.narg('statuses')::text[] IS NULL OR atq.status = ANY(sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('issue_id')::uuid IS NULL OR atq.issue_id = sqlc.narg('issue_id')::uuid)
  AND (sqlc.narg('runtime_id')::uuid IS NULL OR atq.runtime_id = sqlc.narg('runtime_id')::uuid)
  AND (sqlc.narg('since')::timestamptz IS NULL OR atq.created_at >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('cursor_at')::timestamptz IS NULL
       OR atq.created_at < sqlc.narg('cursor_at')::timestamptz
       OR (atq.created_at = sqlc.narg('cursor_at')::timestamptz AND atq.id < sqlc.narg('cursor_id')::uuid))
ORDER BY atq.created_at DESC, atq.id DESC
LIMIT sqlc.arg('lim');

-- name: CountWorkspaceRunsByStatus :many
-- Active runs whatever their age, terminal ones since the window start.
SELECT atq.status, COUNT(*)::bigint AS n FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND (atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'paused', 'deferred')
       OR atq.completed_at >= sqlc.arg('since')::timestamptz)
GROUP BY atq.status;

-- name: SumWorkspaceRunCostSince :one
SELECT COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND atq.created_at >= sqlc.arg('since')::timestamptz;

-- name: ListTaskUsageForTasks :many
SELECT tu.task_id, tu.provider, tu.model, tu.input_tokens, tu.output_tokens, tu.cache_read_tokens, tu.cache_write_tokens, tu.cost_usd_ticks
FROM task_usage tu
WHERE tu.task_id = ANY(sqlc.arg('task_ids')::uuid[])
ORDER BY tu.task_id, tu.model;

-- name: ListPendingApprovalGatesForTasks :many
SELECT * FROM approval_gate_event
WHERE task_id = ANY(sqlc.arg('task_ids')::uuid[]) AND resolved_action IS NULL
ORDER BY created_at ASC;

-- name: ListPendingIssueDecisionsForTasks :many
SELECT * FROM issue_decision
WHERE task_id = ANY(sqlc.arg('task_ids')::uuid[]) AND response IS NULL
ORDER BY created_at ASC;

-- name: ListWaitingIssueGoalsForIssues :many
SELECT * FROM issue_goal
WHERE workspace_id = sqlc.arg('workspace_id') AND issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
  AND status = 'waiting_user' AND question IS NOT NULL;

-- name: ListPendingIssueTransitionRequestsForIssues :many
SELECT * FROM issue_transition_request
WHERE workspace_id = sqlc.arg('workspace_id') AND issue_id = ANY(sqlc.arg('issue_ids')::uuid[]) AND state = 'pending';

-- name: ListActiveWorkspaceTaskIDs :many
-- Everything a kill switch stops: every run that is not over yet.
SELECT atq.id FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'paused', 'deferred')
ORDER BY atq.created_at ASC;
