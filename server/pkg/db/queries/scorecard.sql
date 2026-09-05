-- Scorecards (K25).

-- name: RollupAgentScorecards :execrows
-- Recomputes every (workspace, agent, runtime, UTC day) row for runs that
-- ended in [from, to). A run is accepted when its issue is done now,
-- reopened when the issue left done at least once, without intervention
-- when no member comment and no Decision Card landed on the issue while it
-- ran. Recomputing the same window again writes the same rows.
INSERT INTO agent_scorecard_daily (workspace_id, agent_id, runtime_id, day, runs_total, runs_failed, runs_cancelled, runs_accepted, runs_reopened, runs_no_intervention, cost_usd_ticks_total, updated_at)
SELECT a.workspace_id,
    atq.agent_id,
    COALESCE(atq.runtime_id, '00000000-0000-0000-0000-000000000000'::uuid),
    DATE(atq.completed_at AT TIME ZONE 'UTC') AS day,
    COUNT(*)::int,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int,
    COUNT(*) FILTER (WHERE atq.status = 'cancelled')::int,
    COUNT(*) FILTER (WHERE atq.status = 'completed' AND i.completed_at IS NOT NULL)::int,
    COUNT(*) FILTER (WHERE atq.status = 'completed' AND COALESCE(i.reopen_count, 0) > 0)::int,
    COUNT(*) FILTER (WHERE atq.status = 'completed'
        AND NOT EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = atq.issue_id AND c.author_type = 'member' AND c.created_at >= atq.created_at AND c.created_at <= atq.completed_at)
        AND NOT EXISTS (SELECT 1 FROM issue_decision d WHERE d.issue_id = atq.issue_id AND d.created_at >= atq.created_at AND d.created_at <= atq.completed_at))::int,
    COALESCE(SUM(u.cost), 0)::bigint,
    now()
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
LEFT JOIN LATERAL (SELECT SUM(tu.cost_usd_ticks) AS cost FROM task_usage tu WHERE tu.task_id = atq.id) u ON true
WHERE atq.status IN ('completed', 'failed', 'cancelled')
  AND atq.completed_at >= $1 AND atq.completed_at < $2
GROUP BY a.workspace_id, atq.agent_id, COALESCE(atq.runtime_id, '00000000-0000-0000-0000-000000000000'::uuid), DATE(atq.completed_at AT TIME ZONE 'UTC')
ON CONFLICT (workspace_id, agent_id, runtime_id, day) DO UPDATE SET
    runs_total = EXCLUDED.runs_total,
    runs_failed = EXCLUDED.runs_failed,
    runs_cancelled = EXCLUDED.runs_cancelled,
    runs_accepted = EXCLUDED.runs_accepted,
    runs_reopened = EXCLUDED.runs_reopened,
    runs_no_intervention = EXCLUDED.runs_no_intervention,
    cost_usd_ticks_total = EXCLUDED.cost_usd_ticks_total,
    updated_at = now();

-- name: ListTerminalTaskStatusesSince :many
SELECT DISTINCT status FROM agent_task_queue WHERE completed_at >= $1;

-- name: ListAgentScorecardDays :many
SELECT * FROM agent_scorecard_daily
WHERE workspace_id = $1 AND agent_id = $2 AND day >= $3 AND day < $4
ORDER BY day ASC;

-- name: ListWorkspaceScorecards :many
SELECT agent_id, runtime_id,
    SUM(runs_total)::int AS runs_total,
    SUM(runs_failed)::int AS runs_failed,
    SUM(runs_cancelled)::int AS runs_cancelled,
    SUM(runs_accepted)::int AS runs_accepted,
    SUM(runs_reopened)::int AS runs_reopened,
    SUM(runs_no_intervention)::int AS runs_no_intervention,
    SUM(cost_usd_ticks_total)::bigint AS cost_usd_ticks_total
FROM agent_scorecard_daily
WHERE workspace_id = $1 AND day >= $2 AND day < $3
GROUP BY agent_id, runtime_id
ORDER BY runs_total DESC;
