-- name: ListRoutingCandidateRuntimes :many
-- Hard candidate set for the runtime router (JEF-237): every runtime in the
-- workspace that could legally execute a task for an agent owned by
-- @owner_id right now. Mirrors the ClaimAgentTask authorization fence —
-- online status, fresh heartbeat (same staleness interval as the claim
-- path), and the visibility rule (public runtimes are shared; private ones
-- only their owner's, with ownerless rows kept compatible with ownerless
-- agents exactly like the claim fence).
SELECT * FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND status = 'online'
  AND COALESCE(last_seen_at, updated_at) >=
      now() - make_interval(secs => @runtime_stale_secs::double precision)
  AND (
      visibility = 'public'
      OR (
          visibility = 'private'
          AND (
              owner_id IS NULL
              OR @owner_id::uuid IS NULL
              OR owner_id = @owner_id
          )
      )
  )
ORDER BY created_at ASC;

-- name: GetRoutingStats :many
-- Per-(runtime, provider, model, task_class) run statistics over the trailing
-- window (90 days at the call site), feeding both the runtime router's
-- scoring and the /api/runtimes/routing-stats endpoint. Only terminal runs
-- (completed/failed) count, and only runs that produced at least one
-- task_usage row — provider/model identity comes from that table, so a run
-- without usage cannot be attributed to a model at all. success_count counts
-- 'completed'; samples counts both. Cost and duration come back as
-- sum + sample-count pairs (not AVG) so the Go layer can tell "no priced /
-- no started runs" (count = 0) apart from a genuine zero average without
-- fighting sqlc's non-nullable AVG inference. Provider is LOWER()-normalized
-- like the dashboard rollups so mixed-case historical rows merge. Each run
-- is attributed to exactly one provider/model (its dominant usage row) so a
-- multi-model run is one sample, and the runtime join is bounded by the
-- agent's workspace so a cross-workspace runtime row cannot leak its name.
SELECT
    atq.runtime_id,
    r.name AS runtime_name,
    LOWER(tu.provider) AS provider,
    tu.model,
    atq.task_class,
    COUNT(*)::int AS samples,
    COUNT(*) FILTER (WHERE atq.status = 'completed')::int AS success_count,
    -- How much of this bucket came from a deliberate benchmark (JEF-276)
    -- rather than from ordinary work, so the dashboard can say where the
    -- evidence for a pair comes from.
    COUNT(*) FILTER (WHERE atq.leg_role = 'benchmark')::int AS benchmark_samples,
    COUNT(tu.cost_usd_ticks)::int AS cost_samples,
    COALESCE(SUM(tu.cost_usd_ticks) FILTER (WHERE tu.cost_usd_ticks IS NOT NULL), 0)::float8
        AS total_cost_usd_ticks,
    COUNT(atq.started_at)::int AS duration_samples,
    COALESCE(SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at))) FILTER (
        WHERE atq.started_at IS NOT NULL
    ), 0)::float8 AS total_duration_secs
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
JOIN agent_runtime r ON r.id = atq.runtime_id AND r.workspace_id = a.workspace_id
JOIN LATERAL (
    -- One attribution per run: a task that consumed several models would
    -- otherwise count one sample per model and SUM(samples) would exceed
    -- the number of runs. The dominant usage row (highest cost, then
    -- tokens) names the provider/model; the cost is the run's total.
    SELECT
        ((array_agg(u.provider ORDER BY u.cost_usd_ticks DESC NULLS LAST,
            (u.input_tokens + u.output_tokens) DESC, u.id))[1])::text AS provider,
        ((array_agg(u.model ORDER BY u.cost_usd_ticks DESC NULLS LAST,
            (u.input_tokens + u.output_tokens) DESC, u.id))[1])::text AS model,
        SUM(u.cost_usd_ticks)::bigint AS cost_usd_ticks
    FROM task_usage u
    WHERE u.task_id = atq.id
) tu ON tu.model IS NOT NULL
WHERE a.workspace_id = @workspace_id
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
  -- Per-leg accounting (JEF-274): a review-like leg judges someone else's
  -- work, so counting it as a sample of the worker's task class measures the
  -- wrong job — a reviewer's cost, duration and success rate say nothing
  -- about how a runtime performs on a bugfix. Retry, fallback, revision and
  -- escalation legs DO count: they are real attempts at the class.
  -- A benchmark leg (JEF-276) is deliberately absent from that list: it is
  -- the same exam, pinned to one (runtime, model) candidate so its outcome is
  -- evidence about that pair. Excluding it would throw away the only runs
  -- deliberately produced to measure a policy.
  AND atq.leg_role NOT IN ('review', 'critique', 'answer', 'watchdog', 'eval')
GROUP BY atq.runtime_id, r.name, LOWER(tu.provider), tu.model, atq.task_class
ORDER BY atq.runtime_id, LOWER(tu.provider), tu.model, atq.task_class;
