-- name: ListBudgetPolicies :many
SELECT * FROM budget_policy
WHERE workspace_id = @workspace_id
ORDER BY scope_type, created_at, id;

-- name: ListApplicableBudgetPolicies :many
SELECT * FROM budget_policy
WHERE workspace_id = @workspace_id
  AND (
    scope_type = 'workspace'
    OR (scope_type = 'project' AND scope_id = sqlc.narg('project_id')::uuid)
    OR (scope_type = 'agent' AND scope_id = @agent_id)
  )
ORDER BY scope_type, id;

-- name: GetBudgetPolicyInWorkspace :one
SELECT * FROM budget_policy
WHERE id = @id AND workspace_id = @workspace_id;

-- name: CreateBudgetPolicy :one
INSERT INTO budget_policy (
  id, workspace_id, scope_type, scope_id, limit_usd_ticks,
  period, warn_bps, action, created_by
) VALUES (
  @id, @workspace_id, @scope_type, sqlc.narg('scope_id'), @limit_usd_ticks,
  @period, @warn_bps, @action, @created_by
)
RETURNING *;

-- name: UpdateBudgetPolicy :one
UPDATE budget_policy
SET limit_usd_ticks = @limit_usd_ticks,
    period = @period,
    warn_bps = @warn_bps,
    action = @action,
    revision = revision + 1,
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id AND revision = @revision
RETURNING *;

-- name: DeleteBudgetPolicy :one
DELETE FROM budget_policy
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: DeleteBudgetOverridesForPolicy :exec
DELETE FROM budget_override WHERE policy_id = @policy_id;

-- name: DeleteBudgetReservationsForPolicy :exec
DELETE FROM budget_reservation WHERE policy_id = @policy_id;

-- name: DeleteBudgetPeriodsForPolicy :exec
DELETE FROM budget_period WHERE policy_id = @policy_id;

-- name: EnsureBudgetPeriod :one
INSERT INTO budget_period (policy_id, period_start, period_end)
VALUES (@policy_id, @period_start, @period_end)
ON CONFLICT (policy_id, period_start, period_end) DO UPDATE
SET updated_at = budget_period.updated_at
RETURNING *;

-- name: GetBudgetPeriod :one
SELECT * FROM budget_period
WHERE policy_id = @policy_id
  AND period_start = @period_start
  AND period_end = @period_end;

-- name: GetActiveBudgetOverride :one
SELECT * FROM budget_override
WHERE workspace_id = @workspace_id
  AND policy_id = @policy_id
  AND expires_at > @now
ORDER BY expires_at DESC
LIMIT 1;

-- name: CreateBudgetOverride :one
INSERT INTO budget_override (id, workspace_id, policy_id, granted_by, reason, expires_at)
VALUES (@id, @workspace_id, @policy_id, @granted_by, @reason, @expires_at)
RETURNING *;

-- name: GetBudgetReservationByKey :one
SELECT * FROM budget_reservation
WHERE policy_id = @policy_id
  AND period_start = @period_start
  AND period_end = @period_end
  AND idempotency_key = @idempotency_key
  AND state <> 'released';

-- name: CreateBudgetReservation :one
INSERT INTO budget_reservation (
  id, policy_id, period_start, period_end, task_id,
  estimate_usd_ticks, idempotency_key
) VALUES (
  @id, @policy_id, @period_start, @period_end, @task_id,
  @estimate_usd_ticks, @idempotency_key
)
RETURNING *;

-- name: IncrementBudgetReserved :one
UPDATE budget_period
SET reserved_usd_ticks = reserved_usd_ticks + @estimate_usd_ticks,
    updated_at = now()
WHERE policy_id = @policy_id
  AND period_start = @period_start
  AND period_end = @period_end
RETURNING *;

-- name: ListReservedBudgetReservationsByTask :many
SELECT * FROM budget_reservation
WHERE task_id = @task_id AND state = 'reserved'
ORDER BY policy_id;

-- name: ConsumeBudgetReservation :one
WITH locked AS (
  SELECT * FROM budget_reservation
  WHERE budget_reservation.id = @reservation_id
    AND budget_reservation.state = 'reserved'
  FOR UPDATE
), changed AS (
  UPDATE budget_reservation AS reservation
  SET state = 'consumed', actual_usd_ticks = @actual_usd_ticks, finalized_at = now()
  FROM locked
  WHERE reservation.id = locked.id
  RETURNING locked.policy_id, locked.period_start, locked.period_end, locked.estimate_usd_ticks
)
UPDATE budget_period AS period
SET reserved_usd_ticks = GREATEST(0, reserved_usd_ticks - changed.estimate_usd_ticks),
    spent_usd_ticks = spent_usd_ticks + @actual_usd_ticks,
    updated_at = now()
FROM changed
WHERE period.policy_id = changed.policy_id
  AND period.period_start = changed.period_start
  AND period.period_end = changed.period_end
RETURNING period.*;

-- name: ReleaseBudgetReservation :one
WITH locked AS (
  SELECT * FROM budget_reservation
  WHERE budget_reservation.id = @reservation_id
    AND budget_reservation.state = 'reserved'
  FOR UPDATE
), changed AS (
  UPDATE budget_reservation AS reservation
  SET state = 'released', finalized_at = now()
  FROM locked
  WHERE reservation.id = locked.id
  RETURNING locked.policy_id, locked.period_start, locked.period_end, locked.estimate_usd_ticks
)
UPDATE budget_period AS period
SET reserved_usd_ticks = GREATEST(0, reserved_usd_ticks - changed.estimate_usd_ticks),
    updated_at = now()
FROM changed
WHERE period.policy_id = changed.policy_id
  AND period.period_start = changed.period_start
  AND period.period_end = changed.period_end
RETURNING period.*;

-- name: MarkBudgetWarnNotified :one
UPDATE budget_period
SET warn_notified_at = now(), updated_at = now()
WHERE policy_id = @policy_id
  AND period_start = @period_start
  AND period_end = @period_end
  AND warn_notified_at IS NULL
RETURNING *;

-- name: MarkBudgetBlockNotified :one
UPDATE budget_period
SET block_notified_at = now(), updated_at = now()
WHERE policy_id = @policy_id
  AND period_start = @period_start
  AND period_end = @period_end
  AND block_notified_at IS NULL
RETURNING *;

-- name: ListRecentAgentTaskUsageForBudget :many
WITH recent_tasks AS (
  SELECT id
  FROM agent_task_queue
  WHERE agent_id = @agent_id AND status = 'completed'
  ORDER BY completed_at DESC NULLS LAST, id DESC
  LIMIT @run_limit
)
SELECT usage.task_id, usage.provider, usage.model,
       usage.input_tokens, usage.output_tokens,
       usage.cache_read_tokens, usage.cache_write_tokens,
       usage.cost_usd_ticks
FROM task_usage usage
JOIN recent_tasks task ON task.id = usage.task_id
ORDER BY usage.task_id, usage.provider, usage.model;

-- name: ListTaskUsageForBudget :many
SELECT task_id, provider, model, input_tokens, output_tokens,
       cache_read_tokens, cache_write_tokens, cost_usd_ticks
FROM task_usage
WHERE task_id = @task_id
ORDER BY provider, model;

-- name: GetTaskBudgetScope :one
SELECT task.id AS task_id,
       agent.workspace_id,
       task.agent_id,
       COALESCE(issue.project_id, autopilot.project_id,
         NULLIF(task.context->>'project_id', '')::uuid) AS project_id
FROM agent_task_queue task
JOIN agent ON agent.id = task.agent_id
LEFT JOIN issue ON issue.id = task.issue_id
LEFT JOIN autopilot_run run ON run.id = task.autopilot_run_id
LEFT JOIN autopilot ON autopilot.id = run.autopilot_id
WHERE task.id = @task_id;

-- name: GetNextQueuedTaskForBudget :one
SELECT * FROM agent_task_queue task
WHERE task.agent_id = @agent_id
  AND task.runtime_id = @runtime_id
  AND task.status = 'queued'
  AND task.wait_reason IS DISTINCT FROM 'budget_paused'
ORDER BY task.priority DESC, task.created_at, task.id
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: GetOldestBudgetPausedTask :one
SELECT * FROM agent_task_queue task
WHERE task.agent_id = @agent_id
  AND task.runtime_id = @runtime_id
  AND task.status = 'queued'
  AND task.wait_reason = 'budget_paused'
ORDER BY task.priority DESC, task.created_at, task.id
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: SetTaskBudgetWaitReason :exec
UPDATE agent_task_queue
SET wait_reason = sqlc.narg('wait_reason')
WHERE id = @task_id AND status = 'queued';

-- name: ListRecoverableBudgetReservations :many
SELECT reservation.*
FROM budget_reservation reservation
LEFT JOIN agent_task_queue task ON task.id = reservation.task_id
WHERE reservation.state = 'reserved'
  AND reservation.created_at < @created_before
  AND (task.id IS NULL OR task.status IN ('completed', 'failed', 'cancelled'))
ORDER BY reservation.created_at
LIMIT @row_limit;
