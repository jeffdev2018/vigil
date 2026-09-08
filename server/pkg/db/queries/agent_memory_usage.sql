-- name: GetAgentMemoryUsage :one
-- One statement gives coverage and per-version counts from the same snapshot.
-- A started task is counted once; reclaims before start only have one receipt.
WITH runs AS MATERIALIZED (
    SELECT task.id, task.started_at, task.memory_context,
        COALESCE(
            task.memory_context->>'agent_status' IN ('loaded', 'unavailable')
            AND jsonb_typeof(task.memory_context->'agent_versions') = 'array'
            AND (task.memory_context->>'dispatched_at')::timestamptz = task.dispatched_at,
            false
        ) AS recorded
    FROM agent_task_queue task
    JOIN agent ON agent.id = task.agent_id
    WHERE agent.id = sqlc.arg(agent_id) AND agent.workspace_id = sqlc.arg(workspace_id)
        AND task.chat_session_id IS NULL
        AND task.trigger_evidence_kind IS DISTINCT FROM 'chat'
        -- Old orphaned chats cannot be distinguished from quick-create runs.
        -- Require positive non-chat provenance; never publish ambiguous counts.
        AND CASE WHEN task.memory_context ? 'is_chat'
            THEN task.memory_context->'is_chat' = 'false'::jsonb
            ELSE task.trigger_evidence_kind IN (
                'comment', 'issue_assignment', 'autopilot_run', 'rule_version', 'rerun', 'delegated_failure'
            )
        END
        AND task.started_at >= sqlc.arg(since)::timestamptz
        AND task.started_at < sqlc.arg(until)::timestamptz
), version_counts AS (
    SELECT version->>'id' AS memory_id, (version->>'revision')::integer AS revision,
        count(DISTINCT runs.id)::bigint AS prepared_runs, max(runs.started_at) AS last_started_at
    FROM runs CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN recorded AND memory_context->>'agent_status' = 'loaded'
            THEN memory_context->'agent_versions' ELSE '[]'::jsonb END
    ) version
    GROUP BY version->>'id', (version->>'revision')::integer
)
SELECT
    count(*)::bigint AS started_runs,
    count(*) FILTER (WHERE recorded)::bigint AS recorded_runs,
    count(*) FILTER (WHERE NOT recorded)::bigint AS unrecorded_runs,
    count(*) FILTER (WHERE recorded AND memory_context->>'agent_status' = 'unavailable')::bigint AS load_failed_runs,
    count(*) FILTER (WHERE recorded AND memory_context->>'agent_status' = 'loaded'
        AND memory_context->'agent_versions' <> '[]'::jsonb)::bigint AS runs_with_agent_memory,
    COALESCE((SELECT jsonb_agg(jsonb_build_object(
        'memory_id', memory_id, 'revision', revision, 'prepared_runs', prepared_runs,
        'last_started_at', last_started_at
    ) ORDER BY memory_id, revision) FROM version_counts), '[]'::jsonb)::jsonb AS versions
FROM runs;
