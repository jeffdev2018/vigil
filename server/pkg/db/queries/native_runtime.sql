-- name: SeedNativeRuntimes :execrows
-- Idempotent per-workspace singleton row for the native runtime. Runs from the
-- server tick so a workspace created after migration 826 still gets its row
-- without hooking workspace creation. Public + workspace-owner, matching
-- migration 831: canUseRuntimeForAgent refuses a private ownerless runtime,
-- so those two columns are what make the row usable for agent creation.
INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, visibility, owner_id)
SELECT w.id, 'native', 'Native runtime', 'native', 'native', 'online', 'in-server agent runtime',
       'public',
       (SELECT m.user_id FROM member m
        WHERE m.workspace_id = w.id AND m.role = 'owner'
        ORDER BY m.created_at, m.user_id
        LIMIT 1)
FROM workspace w
WHERE NOT EXISTS (
    SELECT 1 FROM agent_runtime r
    WHERE r.workspace_id = w.id AND r.runtime_mode = 'native'
);

-- name: HeartbeatNativeRuntimes :execrows
-- ClaimAgentTask requires status='online' and a last_seen_at fresher than the
-- claim freshness window, so the server tick keeps its own runtimes eligible
-- exactly the way a daemon's poll does.
UPDATE agent_runtime
SET last_seen_at = now(), status = 'online', updated_at = now()
WHERE runtime_mode = 'native' AND daemon_id = 'native';

-- name: ListNativeRuntimes :many
SELECT * FROM agent_runtime
WHERE runtime_mode = 'native' AND daemon_id = 'native'
ORDER BY created_at;

-- name: ListRecentRunSummariesForIssue :many
-- Continuity (N03): the summaries of the last terminated runs on an issue,
-- newest first, so a follow-up run opens already knowing what its predecessors
-- did instead of starting from zero. Failed runs count too — their result is
-- empty but their presence is context; the caller filters what it renders.
SELECT id, result FROM agent_task_queue
WHERE issue_id = $1
  AND id <> $2
  AND status IN ('completed', 'failed')
ORDER BY completed_at DESC NULLS LAST
LIMIT $3;

-- name: CreateSubagentTask :one
-- Sub-agent runs (long tasks, brick 5): a native run delegates a bounded
-- piece of work to an isolated in-process loop. The sub-run is its own task
-- row so its transcript, usage and cost land where every run's do, as a
-- 'subagent' leg of the parent's workflow. It never waits in the queue: it
-- starts running the moment the parent asks and is settled by the parent.
INSERT INTO agent_task_queue (
    id, agent_id, issue_id, status, priority, runtime_id, dispatched_at, started_at,
    trigger_summary, leg_role, workflow_root_task_id, delegated_from_task_id,
    accountable_user_id, originator_user_id
)
VALUES ($1, $2, $3, 'running', 0, $4, now(), now(), $5, 'subagent', $6, $7, $8, $9)
RETURNING *;

-- name: SettleSubagentTask :one
UPDATE agent_task_queue
SET status = $2, result = $3, error = $4, completed_at = now()
WHERE id = $1
RETURNING *;
