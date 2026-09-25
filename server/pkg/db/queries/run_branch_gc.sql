-- Dead run-branch garbage collection (JEF-388).
--
-- A terminal run whose delivered branch was neither promoted nor discarded
-- leaves a branch (and usually a worktree) on the daemon's machine forever.
-- These queries back the dry-run plan endpoint, the human-confirmed batch
-- discard, and the branch_gc_tick sweep; all three share this candidate set.

-- name: ListDeadBranchRuns :many
-- The dead-branch set of one workspace: terminal, branch named, neither
-- promoted nor discarded. Runtime and issue are LEFT JOINed because a dead
-- branch is exactly the kind of row whose runtime may have been unregistered
-- since. action_pending mirrors the 409 in-flight guard so the caller marks
-- the entry without a second query per run. older_than additionally bounds
-- completed_at for the sweep's TTL; the plan passes NULL. Bounded so a
-- long-neglected workspace cannot turn one request into an unbounded scan.
SELECT t.id           AS task_id,
       t.issue_id     AS issue_id,
       i.number       AS issue_number,
       i.title        AS issue_title,
       t.branch_name  AS branch_name,
       t.runtime_id   AS runtime_id,
       r.name         AS runtime_name,
       r.status       AS runtime_status,
       r.last_seen_at AS runtime_last_seen_at,
       r.metadata     AS runtime_metadata,
       t.completed_at AS finished_at,
       EXISTS (
           SELECT 1 FROM run_branch_action_request b
           WHERE b.task_id = t.id AND b.status IN ('pending', 'claimed')
       )              AS action_pending
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
LEFT JOIN issue i ON i.id = t.issue_id
LEFT JOIN agent_runtime r ON r.id = t.runtime_id
WHERE a.workspace_id = sqlc.arg('workspace_id')
  AND t.status IN ('completed', 'failed', 'cancelled')
  AND t.branch_name IS NOT NULL AND btrim(t.branch_name) <> ''
  AND t.promoted_at IS NULL
  AND t.discarded_at IS NULL
  AND (sqlc.narg('older_than')::timestamptz IS NULL OR t.completed_at < sqlc.narg('older_than')::timestamptz)
ORDER BY t.completed_at DESC NULLS LAST, t.id DESC
LIMIT sqlc.arg('lim');

-- name: ListWorkspacesWithBranchGCEnabled :many
-- The sweep's workspace set, filtered in SQL so a workspace that never opted
-- in is never read: an absent branch_gc key (or absent enabled flag) yields
-- NULL here, not 'true'. The Go-side parse is the lenient layer on top.
SELECT id, settings FROM workspace
WHERE settings->'branch_gc'->>'enabled' = 'true'
ORDER BY created_at ASC;
