-- Sharded refactoring campaigns (K42).

-- name: CreateRefactorCampaign :one
INSERT INTO refactor_campaign (id, workspace_id, issue_id, fanout_batch_id, name, target_branch, started_by)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: AddCampaignShard :one
INSERT INTO campaign_shard (id, refactor_campaign_id, workspace_id, fanout_member_id, child_issue_id, task_id, assignee_agent_id, description, branch_name, merge_position)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING *;

-- name: GetRefactorCampaign :one
SELECT * FROM refactor_campaign WHERE id = $1;

-- name: GetLatestRefactorCampaignForIssue :one
SELECT * FROM refactor_campaign WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1;

-- name: ListCampaignShards :many
SELECT s.*, t.status AS task_status, m.outcome AS run_outcome FROM campaign_shard s
JOIN agent_task_queue t ON t.id = s.task_id
JOIN fanout_batch_member m ON m.id = s.fanout_member_id
WHERE s.refactor_campaign_id = $1 ORDER BY s.merge_position;

-- name: GetCampaignShard :one
SELECT * FROM campaign_shard WHERE id = $1;

-- name: GetCampaignShardForFanoutMember :one
SELECT * FROM campaign_shard WHERE fanout_member_id = $1;

-- name: GetRebasingCampaignShardForTask :one
SELECT * FROM campaign_shard
WHERE merge_status = 'rebasing' AND merge_task_id IN (sqlc.arg(task_id), sqlc.arg(root_task_id))
LIMIT 1;

-- name: SetCampaignShardMergeStatus :one
UPDATE campaign_shard SET merge_status = $2, merge_task_id = COALESCE(sqlc.narg(merge_task_id), merge_task_id), blockers = $3, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: SetRefactorCampaignStatus :one
UPDATE refactor_campaign SET status = $2, completed_at = CASE WHEN $2 IN ('completed', 'failed') THEN now() ELSE completed_at END
WHERE id = $1 RETURNING *;

-- name: PurgeWorkspaceCampaignShards :exec
DELETE FROM campaign_shard WHERE workspace_id = $1;

-- name: PurgeWorkspaceRefactorCampaigns :exec
DELETE FROM refactor_campaign WHERE workspace_id = $1;
