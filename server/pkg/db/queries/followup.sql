-- Follow-ups (OS plan, réveil programmé): a deferred agent task tagged
-- trigger_evidence_kind = 'followup'. The note rides in trigger_summary
-- ("Follow-up: <note>"); trigger_evidence_ref_id is who scheduled it (a
-- member's user id or an agent id); originator_user_id is set for members.

-- name: ListIssueFollowups :many
SELECT t.id, t.issue_id, t.agent_id, a.name AS agent_name, t.fire_at, t.trigger_summary,
       t.created_at, t.originator_user_id, t.trigger_evidence_ref_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE t.issue_id = $1 AND a.workspace_id = $2
  AND t.status = 'deferred' AND t.trigger_evidence_kind = 'followup'
ORDER BY t.fire_at ASC;

-- name: GetIssueFollowup :one
SELECT t.id, t.issue_id, t.agent_id, a.name AS agent_name, t.fire_at, t.trigger_summary,
       t.created_at, t.originator_user_id, t.trigger_evidence_ref_id, t.status
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE t.id = $1 AND t.issue_id = $2 AND a.workspace_id = $3
  AND t.trigger_evidence_kind = 'followup';

-- name: ListWorkspaceFollowupsBetween :many
SELECT t.id, t.issue_id, i.number AS issue_number, i.title AS issue_title,
       t.agent_id, a.name AS agent_name, t.fire_at, t.trigger_summary
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
JOIN issue i ON i.id = t.issue_id
WHERE a.workspace_id = $1 AND t.status = 'deferred' AND t.trigger_evidence_kind = 'followup'
  AND t.fire_at >= sqlc.arg('since') AND t.fire_at < sqlc.arg('until')
ORDER BY t.fire_at ASC;

-- name: CountAgentFollowupsSince :one
SELECT count(*) FROM agent_task_queue
WHERE agent_id = $1 AND trigger_evidence_kind = 'followup' AND created_at >= $2;

-- name: CountWorkspaceFollowupsSince :one
SELECT count(*) FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = $1 AND t.trigger_evidence_kind = 'followup' AND t.created_at >= $2;
