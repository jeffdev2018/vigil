-- Dashboard vélocité mixte (JEF-251): throughput hebdo, cycle time médian et
-- coût par issue fermée, split member/agent. Lecture seule sur les tables
-- existantes; une issue fermée sans run n'a pas de coût et est absente de la
-- série de coût (même convention que K04).

-- name: ListVelocityWeeklyThroughput :many
-- Issues complétées dans la fenêtre, bucketées par semaine ISO (lundi),
-- groupées par assignee_type. Les autres valeurs (ex. NULL) sont ignorées.
SELECT date_trunc('week', i.completed_at)::date AS week_start,
    COUNT(*) FILTER (WHERE i.assignee_type = 'member')::bigint AS member_count,
    COUNT(*) FILTER (WHERE i.assignee_type = 'agent')::bigint AS agent_count
FROM issue i
WHERE i.workspace_id = @workspace_id
  AND i.completed_at IS NOT NULL
  AND i.completed_at >= @period_start AND i.completed_at < @period_end
  AND i.assignee_type IN ('member', 'agent')
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
GROUP BY 1
ORDER BY 1;

-- name: GetVelocityCycleTime :one
-- Médiane de completed_at - created_at en jours, par assignee_type, sur la
-- fenêtre. COALESCE à 0 quand la fenêtre n'a aucune issue du type (sqlc ne
-- génère pas de pointeur pour percentile_cont); le handler ne publie la
-- médiane que si le compteur du type est > 0, sinon null côté JSON.
WITH window_issues AS (
    SELECT i.assignee_type,
        EXTRACT(EPOCH FROM i.completed_at - i.created_at) / 86400.0 AS days
    FROM issue i
    WHERE i.workspace_id = @workspace_id
      AND i.completed_at IS NOT NULL
      AND i.completed_at >= @period_start AND i.completed_at < @period_end
      AND i.assignee_type IN ('member', 'agent')
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
)
SELECT
    COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY w.days)
        FILTER (WHERE w.assignee_type = 'member'), 0)::float8 AS member_median_days,
    COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY w.days)
        FILTER (WHERE w.assignee_type = 'agent'), 0)::float8 AS agent_median_days,
    COUNT(*) FILTER (WHERE w.assignee_type = 'member')::bigint AS member_count,
    COUNT(*) FILTER (WHERE w.assignee_type = 'agent')::bigint AS agent_count
FROM window_issues w;

-- name: ListVelocityWeeklyIssueCosts :many
-- Par semaine de completed_at, pour les issues ayant des runs agent
-- (jointure issue → agent_task_queue → task_usage, design K04): somme et
-- moyenne des coûts totaux par issue de la semaine.
WITH issue_costs AS (
    SELECT i.id, i.completed_at,
        COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks
    FROM issue i
    JOIN agent_task_queue atq ON atq.issue_id = i.id
    JOIN task_usage tu ON tu.task_id = atq.id
    WHERE i.workspace_id = @workspace_id
      AND i.completed_at IS NOT NULL
      AND i.completed_at >= @period_start AND i.completed_at < @period_end
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
    GROUP BY i.id, i.completed_at
)
SELECT date_trunc('week', completed_at)::date AS week_start,
    COUNT(*)::bigint AS issue_count,
    SUM(cost_usd_ticks)::bigint AS total_cost_usd_ticks,
    AVG(cost_usd_ticks)::bigint AS mean_cost_usd_ticks
FROM issue_costs
GROUP BY 1
ORDER BY 1;
