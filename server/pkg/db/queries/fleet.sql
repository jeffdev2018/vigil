-- Fleet reads (JEF-12). One query behind GET /api/fleet/status and
-- /api/fleet/history.

-- name: ListFleetAgentActivityDaily :many
-- Per-(agent, UTC calendar day) terminal-task buckets, anchored on
-- completed_at and windowed by @since.
--
-- This exists alongside GetWorkspaceAgentActivity30d rather than reusing it:
-- that feeder's buckets are DATE_TRUNC in the SESSION timezone (built for the
-- Agents-list sparkline, which renders instants), and its window is hard-wired
-- to 30 days. The fleet endpoints promise ?since= and label buckets with a
-- calendar date an LLM consumer can quote — both need a UTC-anchored,
-- caller-windowed aggregation, so the fold-and-trim happens in SQL here.
SELECT
    atq.agent_id,
    (atq.completed_at AT TIME ZONE 'UTC')::date::text AS date,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
GROUP BY atq.agent_id, (atq.completed_at AT TIME ZONE 'UTC')::date
ORDER BY atq.agent_id, date;
