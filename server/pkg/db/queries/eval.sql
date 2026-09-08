-- Eval Lab (K24).

-- name: CreateEvalCase :one
INSERT INTO eval_case (id, workspace_id, source_issue_id, source_issue_number, title, description, criteria, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: ListEvalCases :many
SELECT * FROM eval_case WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 500;

-- name: GetEvalCasesByIDs :many
SELECT * FROM eval_case WHERE workspace_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[]);

-- name: CreateEvalSuite :one
INSERT INTO eval_suite (id, workspace_id, name, case_ids, created_by)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListEvalSuites :many
SELECT * FROM eval_suite WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 500;

-- name: GetEvalSuite :one
SELECT * FROM eval_suite WHERE id = $1;

-- name: CreateEvalRun :one
INSERT INTO eval_run (id, workspace_id, suite_id, agent_id, agent_version_id, started_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: CreateBenchmarkRun :one
-- One run per (runtime, model) candidate of a benchmark (JEF-276). The pin is
-- stored on the run, not derived at claim: the replay tasks are stamped with
-- runtime_id so only that runtime can claim them, and the claim forces model
-- so the candidate is what actually executes.
INSERT INTO eval_run (id, workspace_id, suite_id, agent_id, agent_version_id, started_by,
                      benchmark, runtime_id, model, baseline_run_id)
VALUES ($1, $2, $3, $4, $5, $6, true, sqlc.arg(runtime_id), sqlc.arg(model), sqlc.narg(baseline_run_id))
RETURNING *;

-- name: ListBenchmarkRuns :many
SELECT * FROM eval_run WHERE workspace_id = $1 AND benchmark ORDER BY started_at DESC LIMIT 200;

-- name: GetBenchmarkRunsByIDs :many
SELECT * FROM eval_run
WHERE workspace_id = $1 AND benchmark AND id = ANY(sqlc.arg(ids)::uuid[]);

-- name: GetEvalRun :one
SELECT * FROM eval_run WHERE id = $1;

-- name: ListEvalRuns :many
-- Plain runs only: a benchmark (JEF-276) creates one eval_run per candidate,
-- and listing those here would fill the run history with rows that differ
-- only by a pin this payload does not carry. They have their own history
-- (ListBenchmarkRuns), so the two lists stay disjoint and each is readable.
SELECT * FROM eval_run WHERE workspace_id = $1 AND NOT benchmark ORDER BY started_at DESC LIMIT 200;

-- name: HasRunningEvalRunForSuite :one
SELECT EXISTS (SELECT 1 FROM eval_run WHERE suite_id = $1 AND status = 'running');

-- name: CreateEvalRunCase :one
-- task_class is classified from the case title once, here, rather than
-- re-derived at settlement: the title is in hand and the class of a case does
-- not depend on how its replay went.
INSERT INTO eval_run_case (run_id, case_id, issue_id, task_id, task_class)
VALUES ($1, $2, $3, $4, sqlc.arg(task_class)) RETURNING *;

-- name: ListEvalRunCases :many
SELECT rc.*, c.title AS case_title
FROM eval_run_case rc
LEFT JOIN eval_case c ON c.id = rc.case_id
WHERE rc.run_id = $1
ORDER BY c.created_at ASC;

-- name: GetEvalRunCaseByTask :one
-- The eval case a run belongs to: its own run, or the retry chain of it.
-- Carries the run's pinned version so the claim can override the agent's
-- current configuration without a second query.
SELECT rc.*, r.agent_version_id, r.agent_id, r.workspace_id, r.benchmark, r.model AS run_model
FROM eval_run_case rc
JOIN eval_run r ON r.id = rc.run_id
WHERE rc.task_id IN (sqlc.arg(task_id), sqlc.arg(root_task_id))
LIMIT 1;

-- name: SettleEvalRunCase :one
-- Cost and duration are read from the run itself in the same statement rather
-- than passed in: the settlement already knows the task id, and a second
-- round-trip could observe a task_usage row written between the two reads.
-- Both stay NULL when there is nothing to read — a provider that reported no
-- price and a replay that never started are not zero.
UPDATE eval_run_case SET
    status     = sqlc.arg(status),
    score      = sqlc.narg(score),
    detail     = sqlc.arg(detail),
    task_id    = sqlc.arg(task_id),
    cost_usd_ticks = (
        SELECT SUM(u.cost_usd_ticks)::bigint FROM task_usage u WHERE u.task_id = sqlc.arg(task_id)
    ),
    duration_seconds = (
        SELECT EXTRACT(EPOCH FROM (t.completed_at - t.started_at))::int
        FROM agent_task_queue t
        WHERE t.id = sqlc.arg(task_id) AND t.started_at IS NOT NULL AND t.completed_at IS NOT NULL
    ),
    settled_at = now()
WHERE run_id = sqlc.arg(run_id) AND case_id = sqlc.arg(case_id) AND status = 'pending'
RETURNING *;

-- name: CountPendingEvalRunCases :one
SELECT COUNT(*) FROM eval_run_case WHERE run_id = $1 AND status = 'pending';

-- name: FinishEvalRun :one
UPDATE eval_run SET status = sqlc.arg(status), score = sqlc.narg(score), completed_at = now()
WHERE id = sqlc.arg(id) AND status = 'running' RETURNING *;

-- name: PurgeWorkspaceEvalRunCases :exec
DELETE FROM eval_run_case WHERE run_id IN (SELECT id FROM eval_run WHERE workspace_id = $1);

-- name: PurgeWorkspaceEvalRuns :exec
DELETE FROM eval_run WHERE workspace_id = $1;

-- name: PurgeWorkspaceEvalSuites :exec
DELETE FROM eval_suite WHERE workspace_id = $1;

-- name: PurgeWorkspaceEvalCases :exec
DELETE FROM eval_case WHERE workspace_id = $1;

-- name: PinBenchmarkReplayTask :one
-- Pins one benchmark replay run (JEF-276) to the candidate it measures. The
-- runtime is the pin that matters: ClaimAgentTask already selects on
-- atq.runtime_id, so stamping it here is what makes the task invisible to
-- every other runtime instead of needing a new predicate in the claim. The
-- leg role is what lets the claim fences accept a runtime the agent is not
-- bound to, and what makes GetRoutingStats count the outcome. Restricted to a
-- queued task so a race that already dispatched it is never re-pointed.
UPDATE agent_task_queue
SET runtime_id = sqlc.arg(runtime_id),
    leg_role   = 'benchmark',
    task_class = sqlc.arg(task_class)
WHERE id = sqlc.arg(id) AND status = 'queued'
RETURNING *;
