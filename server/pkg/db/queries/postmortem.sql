-- name: CreatePostmortem :one
-- Insert one postmortem. The unique index on source_task_id (469) makes this
-- idempotent: a redelivered task:failed or a rerun of the pass is a no-op.
INSERT INTO postmortem (
    workspace_id, source_task_id, issue_id, agent_id,
    trigger, failure_reason, summary, root_cause, impact,
    preventive_rules, cost_usd_ticks, llm_generated
)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('source_task_id')::uuid,
    sqlc.narg('issue_id')::uuid,
    sqlc.narg('agent_id')::uuid,
    sqlc.arg('trigger'),
    sqlc.arg('failure_reason'),
    sqlc.arg('summary'),
    sqlc.arg('root_cause'),
    sqlc.arg('impact'),
    sqlc.arg('preventive_rules')::jsonb,
    sqlc.narg('cost_usd_ticks')::bigint,
    sqlc.arg('llm_generated')
)
ON CONFLICT (source_task_id) DO NOTHING
RETURNING *;

-- name: GetPostmortem :one
SELECT * FROM postmortem
WHERE id = $1 AND workspace_id = $2;

-- name: GetPostmortemBySourceTask :one
SELECT * FROM postmortem
WHERE source_task_id = $1;

-- name: ListPostmortems :many
SELECT * FROM postmortem
WHERE workspace_id = $1
  AND state = sqlc.arg('state')
  AND (
      sqlc.narg('cursor_time')::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg('cursor_time')::timestamptz, sqlc.narg('cursor_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('page_limit')::int;

-- name: ResolvePostmortem :one
UPDATE postmortem
SET state = sqlc.arg('state'),
    resolved_at = now(),
    resolved_by_type = 'member',
    resolved_by_id = sqlc.arg('resolved_by')::uuid,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'draft'
RETURNING *;

-- name: CountPostmortemsByState :many
SELECT state, COUNT(*)::bigint AS n
FROM postmortem
WHERE workspace_id = $1
GROUP BY state;
