-- Goal loop (long tasks): per-issue goal and run-chain state.

-- name: GetIssueGoal :one
SELECT * FROM issue_goal WHERE issue_id = $1 AND workspace_id = $2;

-- name: EnsureIssueGoal :one
-- Creates the row on first contact; an existing row is returned untouched.
INSERT INTO issue_goal (id, workspace_id, issue_id, max_continuations)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id) DO UPDATE SET updated_at = issue_goal.updated_at
RETURNING *;

-- name: UpsertIssueGoalText :one
-- A member (or an agent) writes the goal and the ceiling; the chain state is
-- left alone so a rewritten goal does not restart a running chain.
INSERT INTO issue_goal (id, workspace_id, issue_id, goal, max_continuations, set_by_type, set_by_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (issue_id) DO UPDATE SET
    goal = EXCLUDED.goal,
    max_continuations = EXCLUDED.max_continuations,
    set_by_type = EXCLUDED.set_by_type,
    set_by_id = EXCLUDED.set_by_id,
    updated_at = now()
RETURNING *;

-- name: UpdateIssueGoalState :one
UPDATE issue_goal
SET status = $2,
    continuation = $3,
    no_progress = $4,
    last_signature = $5,
    last_outcome = $6,
    last_blocker = $7,
    last_reason = $8,
    next_step = $9,
    evidence = $10,
    question = $11,
    last_run_id = $12,
    done_request_id = $13,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetIssueGoalStatus :one
UPDATE issue_goal SET status = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: SetIssueGoalQuestion :one
-- The run that asks records its question here; the judge reads it back as a
-- deterministic needs_user_input verdict for that run.
UPDATE issue_goal SET question = $2, status = 'waiting_user', updated_at = now() WHERE id = $1 RETURNING *;

-- name: ResetIssueGoalChain :one
-- Resume after a pause or a stop: the chain gets a fresh allowance.
UPDATE issue_goal
SET status = 'active', continuation = 0, no_progress = 0, last_signature = '', question = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AppendTaskResult :exec
-- Merges keys into a settled run's result JSON (the per-run goal verdict).
UPDATE agent_task_queue SET result = COALESCE(result, '{}'::jsonb) || $2::jsonb WHERE id = $1;

-- name: ListQueuedContinuationsForIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = $1 AND leg_role = 'continuation' AND status IN ('queued', 'dispatched', 'deferred');

-- name: PurgeWorkspaceIssueGoals :exec
DELETE FROM issue_goal WHERE workspace_id = $1;
