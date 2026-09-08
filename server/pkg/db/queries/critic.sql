-- Adversarial critic (F25 / JEF-18): the per-agent / per-squad policy and the
-- structured verdicts it produces.

-- name: GetCriticPolicy :one
SELECT * FROM agent_critic_policy
WHERE workspace_id = $1 AND subject_type = $2 AND subject_id = $3;

-- name: UpsertCriticPolicy :one
INSERT INTO agent_critic_policy (
    id, workspace_id, subject_type, subject_id, enabled, critic_agent_id,
    require_distinct_provider, blocking, max_rounds, max_cost_usd_ticks, phases
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (workspace_id, subject_type, subject_id) DO UPDATE SET
    enabled                   = EXCLUDED.enabled,
    critic_agent_id           = EXCLUDED.critic_agent_id,
    require_distinct_provider = EXCLUDED.require_distinct_provider,
    blocking                  = EXCLUDED.blocking,
    max_rounds                = EXCLUDED.max_rounds,
    max_cost_usd_ticks        = EXCLUDED.max_cost_usd_ticks,
    phases                    = EXCLUDED.phases,
    updated_at                = now()
RETURNING *;

-- name: CreateCriticVerdict :one
INSERT INTO agent_critic_verdict (
    id, workspace_id, issue_id, subject_task_id, critic_task_id, phase,
    verdict, reason, summary, findings, round, cost_usd_ticks,
    created_by_type, created_by_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: ListCriticVerdictsForIssue :many
-- Round order, then arrival: the card numbers the rounds, so a stable
-- ascending read is what it wants, not the newest-first the index serves.
SELECT * FROM agent_critic_verdict
WHERE issue_id = $1
ORDER BY round, created_at;

-- name: GetCriticVerdictByCriticTask :one
SELECT * FROM agent_critic_verdict
WHERE critic_task_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: CountCriticVerdictsForIssuePhase :one
-- Rounds already spent on this delivery chain. subject_task_id changes with
-- every relaunch, so the count is taken over the issue and the phase, which is
-- what "round" means to a reader: the nth time this issue was criticised.
SELECT COUNT(*) FROM agent_critic_verdict WHERE issue_id = $1 AND phase = $2;

-- name: SumIssueTaskCostTicks :one
-- Everything the issue has spent, across every run of it. The critic budget is
-- about the issue's total bill, not one leg's.
SELECT COALESCE(SUM(u.cost_usd_ticks), 0)::bigint
FROM task_usage u
JOIN agent_task_queue t ON t.id = u.task_id
WHERE t.issue_id = $1;

-- name: SetTaskCriticContext :one
-- Stamps the critic run with the delivery it is judging. The run itself is the
-- only place this pointer can live: the verdict row does not exist yet, and a
-- critic run that dies without answering must still be recognisable at
-- completion so the fallback can record `no_verdict`.
UPDATE agent_task_queue
SET context = COALESCE(context, '{}'::jsonb) || jsonb_build_object(
        'critic_of_task_id', sqlc.arg(critic_of_task_id)::text,
        'critic_round', sqlc.arg(critic_round)::int,
        'critic_phase', sqlc.arg(critic_phase)::text,
        'critic_blocking', sqlc.arg(critic_blocking)::boolean)
WHERE id = $1
RETURNING *;

-- name: PurgeWorkspaceCriticPolicies :exec
DELETE FROM agent_critic_policy WHERE workspace_id = $1;

-- name: PurgeWorkspaceCriticVerdicts :exec
DELETE FROM agent_critic_verdict WHERE workspace_id = $1;

-- name: GetBlockingCriticRunForIssue :one
-- The critic run an issue is currently waiting on, when its policy is
-- blocking. A run that finished — answered, failed or cancelled — no longer
-- holds anything: the hold releases itself rather than needing a timeout, and
-- a critic that crashes can never strand an issue.
SELECT * FROM agent_task_queue
WHERE issue_id = $1
  AND context ->> 'critic_of_task_id' IS NOT NULL
  AND context ->> 'critic_blocking' = 'true'
  AND status NOT IN ('completed', 'failed', 'cancelled')
ORDER BY created_at DESC
LIMIT 1;
