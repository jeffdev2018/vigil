-- Agent context document drift detection (K56). After a repository's default
-- branch moves, a read-only agent run compares the repo's agent context
-- document (CLAUDE.md / AGENTS.md, plus the conventions doc when present)
-- against what the repository actually declares — Makefile targets,
-- package.json scripts, the top-level tree, the named conventions — and
-- proposes an incremental update. The proposal is reviewed by a human as a
-- DRAFT pull request; nothing is ever committed to the default branch.
--
-- One row per (repository, document) proposal. New sections found by a later
-- scan are merged into the open row rather than opening a second one, so a
-- reviewer sees one accumulating proposal per document instead of a queue of
-- near-duplicates; 732 makes that invariant a constraint.
--
-- No foreign keys by repository convention: workspace teardown deletes these
-- rows explicitly (purge step in handler/workspace.go).
CREATE TABLE IF NOT EXISTS doc_drift_proposal (
    id                 UUID PRIMARY KEY,
    workspace_id       UUID NOT NULL,
    -- Repository URL, the same identifier the repo index (K47) is keyed by.
    repo_identifier    TEXT NOT NULL,
    -- Repo-relative path of the document this proposal updates.
    doc_path           TEXT NOT NULL,
    -- Human summary of what drifted, one section per bullet.
    detected_drift     TEXT NOT NULL DEFAULT '',
    -- The change itself: a unified diff, or replacement text for a section.
    proposed_patch     TEXT NOT NULL DEFAULT '',
    -- Commit of the default branch the drift was observed at.
    detected_at_commit TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'opened_pr', 'dismissed', 'merged')),
    pull_request_url   TEXT NOT NULL DEFAULT '',
    -- The read-only scan run that reported the drift.
    scan_task_id       UUID,
    -- The run that opened the draft pull request, when one was enqueued.
    pr_task_id         UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
