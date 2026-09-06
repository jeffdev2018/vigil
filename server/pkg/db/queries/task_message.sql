-- name: CreateTaskMessage :one
INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateTaskMessages :many
-- Batch variant of CreateTaskMessage: persists a whole daemon-reported batch in
-- ONE statement — therefore one round trip and, more importantly, one commit
-- instead of one per message. Commit acknowledgement (IO:XactSync) is ~94% of
-- this SQL's load in production, so the commit count is the thing being
-- optimized; the round trip is a bonus.
--
-- The rows arrive as parallel arrays rather than as one jsonb document, even
-- though a jsonb document is the tidier Go side. content and output routinely
-- carry tens of KB and occasionally megabytes, and wrapping them in JSON makes
-- the server escape every byte a second time and Postgres parse the whole
-- envelope back out — measured at 1.2x the old per-row insert at 128KB and
-- ~1.8x at 1MB, i.e. a regression on exactly the most expensive requests, which
-- are also the ones least likely to be batched. Native text[] elements are
-- length-prefixed by the wire protocol, so they cost neither pass.
--
-- NULLIF is what makes per-row NULL expressible through a non-nullable []string
-- (the Go type sqlc gives a text[] parameter): it reproduces, exactly, the
-- `pgtype.Text{Valid: x != ""}` mapping the single-row query carries — empty
-- string means SQL NULL. input is passed as text and cast here for the same
-- reason; it is the one column that genuinely has to be parsed as JSON, because
-- it is a jsonb column.
--
-- Callers MUST still run the Postgres text sanitizer first. A NUL anywhere in
-- the batch fails the whole statement (GH #7098) — that is inherent to batching
-- into one statement, not to the parameter shape.
--
-- Atomicity is a deliberate side effect, not just a speedup: the per-message
-- loop this replaces could persist part of a batch and then fail, leaving the
-- transcript with a prefix of the batch and no way to complete it — the daemon
-- does not retry this endpoint. One statement makes the batch all-or-nothing,
-- which buys consistency; a batch that fails is still lost whole, so closing
-- the gap for real needs a retry plus a (task_id, seq) uniqueness rule.
--
-- The ORDER BY is a contract, not decoration. A bare `INSERT ... RETURNING`
-- has no defined row order, and the caller republishes these rows as realtime
-- events in the order they arrive — the per-row loop this replaces implicitly
-- published in request order, so the ordering has to be restored explicitly or
-- subscribers can see a batch out of order. seq is assigned by the daemon and
-- increases within a batch, so it is the request order.
WITH incoming AS (
    -- Several single-argument unnest calls in one SELECT list expand in
    -- lockstep (PostgreSQL 10+ set-returning-function semantics), which is the
    -- same row-wise zip the multi-argument unnest(a, b, ...) form gives — but
    -- sqlc's analyzer only knows the single-argument signature, so this is the
    -- shape that survives code generation.
    SELECT
        unnest(sqlc.arg('ids')::uuid[]) AS id,
        unnest(sqlc.arg('seqs')::int4[]) AS seq,
        unnest(sqlc.arg('types')::text[]) AS type,
        unnest(sqlc.arg('tools')::text[]) AS tool,
        unnest(sqlc.arg('contents')::text[]) AS content,
        unnest(sqlc.arg('inputs')::text[]) AS input,
        unnest(sqlc.arg('outputs')::text[]) AS output
), inserted AS (
    INSERT INTO task_message (id, task_id, seq, type, tool, content, input, output)
    SELECT
        m.id,
        sqlc.arg('task_id')::uuid,
        m.seq,
        m.type,
        NULLIF(m.tool, ''),
        NULLIF(m.content, ''),
        NULLIF(m.input, '')::jsonb,
        NULLIF(m.output, '')
    FROM incoming AS m
    RETURNING *
)
SELECT * FROM inserted ORDER BY seq ASC;

-- name: ListTaskMessages :many
SELECT * FROM task_message
WHERE task_id = $1
ORDER BY seq ASC;

-- name: ListTaskMessagesSince :many
SELECT * FROM task_message
WHERE task_id = $1 AND seq > $2
ORDER BY seq ASC;

-- name: DeleteTaskMessages :exec
DELETE FROM task_message
WHERE task_id = $1;

-- name: ListRecentTaskToolUses :many
-- Drift detection (K40): the run's latest tool calls, oldest first.
SELECT * FROM (
    SELECT * FROM task_message WHERE task_id = $1 AND type IN ('tool_use', 'tool-use') ORDER BY seq DESC LIMIT $2
) recent ORDER BY seq ASC;

-- name: SetTaskDriftReason :exec
UPDATE agent_task_queue SET drift_reason = $2 WHERE id = $1;

-- name: CreateTaskPlanMessage :one
-- Living run plan (F04): the plan is a task_message of type 'plan' whose input
-- holds the checklist. Replacement is "highest seq wins", so the only thing the
-- allocator has to guarantee is a seq that is unique for the task and greater
-- than every earlier plan's.
--
-- seq_floor is what keeps it unique. The daemon numbers its own messages from
-- its in-process counter starting at 1 (daemon.go: `var msgSeq atomic.Int32`),
-- NOT from the database — so a plain MAX(seq)+1 would hand the plan the very
-- number the daemon's next flush is about to use, and the clients' merge-by-seq
-- (mergeTaskMessagesBySeq) would drop one of the two. Allocating plans from a
-- reserved band above anything the daemon can reach removes the collision
-- instead of racing it. Display order does not depend on the number: the
-- transcript slots plan entries chronologically by created_at.
--
-- The aggregate makes this exactly one row even when the task has no messages
-- yet (MAX over an empty set is NULL, and the SELECT still yields one row), so
-- the statement is atomic on its own and needs no surrounding transaction.
INSERT INTO task_message (id, task_id, seq, type, content, input)
SELECT
    sqlc.arg('id')::uuid,
    sqlc.arg('task_id')::uuid,
    GREATEST(COALESCE(MAX(seq), 0), sqlc.arg('seq_floor')::int4) + 1,
    'plan',
    sqlc.arg('content')::text,
    sqlc.arg('input')::jsonb
FROM task_message
WHERE task_id = sqlc.arg('task_id')::uuid
RETURNING *;

-- name: ListLatestTaskPlans :many
-- The current plan of every run in an execution log, in one round trip: DISTINCT ON
-- keeps the highest-seq plan per task, so hydrating a list of runs costs one
-- query rather than one per row.
SELECT DISTINCT ON (task_id) *
FROM task_message
WHERE task_id = ANY(sqlc.arg('task_ids')::uuid[]) AND type = 'plan'
ORDER BY task_id, seq DESC;
