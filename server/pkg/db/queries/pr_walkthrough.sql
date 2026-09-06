-- Narrative pull request walkthrough (F05 / JEF-16).

-- ClaimPrWalkthroughHead is the enqueue fence. The unique index on
-- (pr_source, pr_id, head_sha) makes this statement the atomic "is a run
-- already out for this head?" question, so two triggers firing on one push
-- enqueue at most one run.
--
-- A head that already SETTLED is re-claimable: the DO UPDATE fires only when
-- the row is not pending, which is what makes "retry" work after a failure and
-- "regenerate" work on a ready walkthrough. Groups are deliberately left in
-- place — a re-run that fails must not destroy the narrative the reviewer had.
-- A pending row matches no predicate, returns nothing, and the caller reports
-- the conflict.
-- name: ClaimPrWalkthroughHead :one
INSERT INTO pr_walkthrough (id, workspace_id, issue_id, pr_source, pr_id, head_sha, state)
VALUES ($1, $2, $3, $4, $5, $6, 'pending')
ON CONFLICT (pr_source, pr_id, head_sha) DO UPDATE
SET state = 'pending', error = '', task_id = NULL, updated_at = now()
WHERE pr_walkthrough.state <> 'pending'
RETURNING *;

-- name: GetPrWalkthroughForHead :one
SELECT * FROM pr_walkthrough
WHERE pr_source = $1 AND pr_id = $2 AND head_sha = $3;

-- name: GetPrWalkthroughByTask :one
SELECT * FROM pr_walkthrough WHERE task_id = $1;

-- name: SetPrWalkthroughTask :exec
UPDATE pr_walkthrough SET task_id = $2, updated_at = now() WHERE id = $1;

-- StorePrWalkthroughResult settles one row. It never looks at which head is
-- current: a completion writes into ITS OWN row, so a run that finishes after
-- the PR moved on records what it actually reviewed and leaves the newer head
-- alone. Readers pick the row for the current head.
-- name: StorePrWalkthroughResult :one
UPDATE pr_walkthrough
SET state = $2, groups = $3, truncated = $4, omitted_files = $5, error = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: PurgeWorkspacePrWalkthroughs :exec
DELETE FROM pr_walkthrough WHERE workspace_id = $1;
