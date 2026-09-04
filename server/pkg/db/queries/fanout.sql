-- Fan-out / fan-in (K38).

-- name: CreateFanoutBatch :one
INSERT INTO fanout_batch (id, workspace_id, parent_issue_id, leader_agent_id, expected_count, started_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: AddFanoutBatchMember :one
INSERT INTO fanout_batch_member (id, fanout_batch_id, workspace_id, child_issue_id, task_id, assignee_agent_id, sub_task_description)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: GetFanoutBatch :one
SELECT * FROM fanout_batch WHERE id = $1;

-- name: GetLatestFanoutBatchForIssue :one
SELECT * FROM fanout_batch WHERE parent_issue_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: ListFanoutBatchMembers :many
SELECT m.*, t.status AS task_status FROM fanout_batch_member m
JOIN agent_task_queue t ON t.id = m.task_id
WHERE m.fanout_batch_id = $1 ORDER BY m.id;

-- name: GetFanoutMemberForTask :one
-- The member a run belongs to: its own run, or the retry chain of it.
SELECT m.* FROM fanout_batch_member m
WHERE m.outcome IS NULL AND (m.task_id = sqlc.arg(task_id) OR m.task_id = sqlc.arg(root_task_id))
LIMIT 1;

-- name: SettleFanoutMember :one
UPDATE fanout_batch_member SET outcome = $2, settled_task_id = $3, settled_at = now()
WHERE id = $1 AND outcome IS NULL RETURNING *;

-- name: CountFanoutOutcomes :one
SELECT count(*) FILTER (WHERE outcome = 'completed')::int AS completed, count(*) FILTER (WHERE outcome = 'failed')::int AS failed
FROM fanout_batch_member WHERE fanout_batch_id = $1;

-- name: SettleFanoutBatch :one
UPDATE fanout_batch SET completed_count = $2, failed_count = $3, status = $4, synthesis_task_id = $5, completed_at = now()
WHERE id = $1 AND status = 'pending' RETURNING *;

-- name: UpdateFanoutCounts :exec
UPDATE fanout_batch SET completed_count = $2, failed_count = $3 WHERE id = $1;

-- name: HasRunnableSuccessorForTask :one
SELECT EXISTS (
    SELECT 1 FROM agent_task_queue
    WHERE parent_task_id = $1 AND status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory', 'paused')
);

-- name: PurgeWorkspaceFanoutMembers :exec
DELETE FROM fanout_batch_member WHERE workspace_id = $1;

-- name: PurgeWorkspaceFanoutBatches :exec
DELETE FROM fanout_batch WHERE workspace_id = $1;

-- name: GetLatestTaskForIssue :one
SELECT * FROM agent_task_queue WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1;
