-- Agent context document drift detection (K56). See migration 731 for the
-- table's contract: one row per proposed update to one document of one
-- repository, and at most one row per document in an open state (732).

-- name: CreateDocDriftProposal :one
INSERT INTO doc_drift_proposal (
    id, workspace_id, repo_identifier, doc_path,
    detected_drift, proposed_patch, detected_at_commit, scan_task_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetOpenDocDriftProposalForDoc :one
-- The merge target. A later scan that finds more drift in a document already
-- under review appends to this row instead of opening a rival proposal — the
-- reviewer reads one accumulating patch, not a queue of near-duplicates.
SELECT * FROM doc_drift_proposal
WHERE workspace_id = $1
  AND repo_identifier = $2
  AND doc_path = $3
  AND status IN ('draft', 'opened_pr')
ORDER BY created_at DESC
LIMIT 1;

-- name: AppendDocDriftProposal :one
-- The merge write. The merged text is composed in Go so the append rule lives
-- in one readable place; this statement only stores it and moves the clock.
UPDATE doc_drift_proposal
SET detected_drift = sqlc.arg('detected_drift'),
    proposed_patch = sqlc.arg('proposed_patch'),
    detected_at_commit = sqlc.arg('detected_at_commit'),
    scan_task_id = sqlc.arg('scan_task_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ListDocDriftProposals :many
SELECT * FROM doc_drift_proposal
WHERE workspace_id = $1
ORDER BY updated_at DESC
LIMIT 100;

-- name: GetDocDriftProposal :one
SELECT * FROM doc_drift_proposal WHERE id = $1;

-- name: GetDocDriftProposalByPRTask :one
SELECT * FROM doc_drift_proposal WHERE pr_task_id = $1;

-- name: SetDocDriftProposalPRTask :one
UPDATE doc_drift_proposal
SET pr_task_id = sqlc.arg('pr_task_id'), updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: SetDocDriftProposalPullRequest :one
-- The pull-request run reported a URL: the proposal is now in front of a human
-- in the forge. Guarded on 'draft' so a repeated completion callback is
-- idempotent.
UPDATE doc_drift_proposal
SET status = 'opened_pr', pull_request_url = sqlc.arg('pull_request_url'), updated_at = now()
WHERE id = sqlc.arg('id') AND status = 'draft'
RETURNING *;

-- name: DismissDocDriftProposal :one
-- Dismissing releases the document: the partial unique index only covers the
-- open states, so the next scan that still sees drift opens a fresh proposal
-- rather than being silently deduplicated against a rejected one.
UPDATE doc_drift_proposal
SET status = 'dismissed', updated_at = now()
WHERE id = sqlc.arg('id') AND status IN ('draft', 'opened_pr')
RETURNING *;

-- name: GetDocDriftHostIssue :one
-- The one housekeeping issue every drift run hangs off. Recognised by its
-- origin: it is the only 'doc_drift' issue with no origin_id.
SELECT * FROM issue
WHERE workspace_id = $1 AND origin_type = 'doc_drift' AND origin_id IS NULL
ORDER BY created_at ASC
LIMIT 1;

-- name: PurgeWorkspaceDocDriftProposals :exec
DELETE FROM doc_drift_proposal WHERE workspace_id = $1;
