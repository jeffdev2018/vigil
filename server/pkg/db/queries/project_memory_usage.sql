-- name: GetProjectMemoryUsage :one
-- Coverage of prepared project-memory context on retained, started non-chat
-- runs for issues of this project. Measures preparation, not model adherence.
-- Runs are scoped by issue.project_id, so project_version rows are counted
-- whenever a structured version was prepared (no second UUID param needed).
WITH runs AS MATERIALIZED (
    SELECT task.id, task.started_at, task.memory_context,
        COALESCE(
            task.memory_context->>'agent_status' IN ('loaded', 'unavailable')
            AND jsonb_typeof(task.memory_context->'agent_versions') = 'array'
            AND (task.memory_context->>'dispatched_at')::timestamptz = task.dispatched_at,
            false
        ) AS recorded
    FROM agent_task_queue task
    JOIN issue ON issue.id = task.issue_id
    WHERE issue.project_id = sqlc.arg(project_id)
        AND issue.workspace_id = sqlc.arg(workspace_id)
        AND task.chat_session_id IS NULL
        AND task.trigger_evidence_kind IS DISTINCT FROM 'chat'
        AND CASE WHEN task.memory_context ? 'is_chat'
            THEN task.memory_context->'is_chat' = 'false'::jsonb
            ELSE task.trigger_evidence_kind IN (
                'comment', 'issue_assignment', 'autopilot_run', 'rule_version', 'rerun', 'delegated_failure'
            )
        END
        AND task.started_at >= sqlc.arg(since)::timestamptz
        AND task.started_at < sqlc.arg(until)::timestamptz
), version_counts AS (
    SELECT version->>'id' AS project_id, (version->>'revision')::integer AS revision,
        count(DISTINCT runs.id)::bigint AS prepared_runs, max(runs.started_at) AS last_started_at
    FROM runs
    CROSS JOIN LATERAL (
        SELECT memory_context->'project_version' AS version
    ) AS project_version
    WHERE recorded
        AND jsonb_typeof(version) = 'object'
        AND COALESCE(version->>'id', '') <> ''
        AND (version->>'revision')::integer >= 1
    GROUP BY version->>'id', (version->>'revision')::integer
)
SELECT
    count(*)::bigint AS started_runs,
    count(*) FILTER (WHERE recorded)::bigint AS recorded_runs,
    count(*) FILTER (WHERE NOT recorded)::bigint AS unrecorded_runs,
    count(*) FILTER (
        WHERE recorded
            AND jsonb_typeof(memory_context->'project_version') = 'object'
            AND COALESCE(memory_context->'project_version'->>'id', '') <> ''
            AND (memory_context->'project_version'->>'revision')::integer >= 1
    )::bigint AS runs_with_project_memory,
    COALESCE((SELECT jsonb_agg(jsonb_build_object(
        'project_id', project_id, 'revision', revision, 'prepared_runs', prepared_runs,
        'last_started_at', last_started_at
    ) ORDER BY project_id, revision) FROM version_counts), '[]'::jsonb)::jsonb AS versions
FROM runs;
