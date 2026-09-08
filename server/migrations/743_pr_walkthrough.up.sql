-- Narrative pull request walkthrough (F05 / JEF-16). For a pull request linked
-- to an issue, a read-only agent run reads the unified diff and groups the
-- change into an ordered narrative — core / test / generated / noise — each
-- group carrying a rationale and a per-hunk explanation. The reviewer reads the
-- story of the change instead of reconstructing it from a file list.
--
-- Keyed by (pr_source, pr_id, head_sha) rather than by pull request: a
-- walkthrough describes ONE revision of the diff. When the PR head moves the
-- old row stays as the record of what that head looked like, and a completion
-- arriving late for a stale head settles into its own row without ever
-- overwriting the current head's. Readers ask for the current head and get
-- `pending` while its run is still out.
--
-- No foreign keys by repository convention: workspace teardown deletes these
-- rows explicitly (purge step in handler/workspace.go).
CREATE TABLE IF NOT EXISTS pr_walkthrough (
    id            UUID PRIMARY KEY,
    workspace_id  UUID NOT NULL,
    -- The issue the pull request is linked to. A PR with no linked issue never
    -- gets a walkthrough — the issue is what gives the run its context.
    issue_id      UUID NOT NULL,
    -- Which table pr_id points at: github_pull_request or vcs_pull_request.
    pr_source     TEXT NOT NULL CHECK (pr_source IN ('github', 'vcs')),
    pr_id         UUID NOT NULL,
    -- The commit this walkthrough describes. The row's identity, not metadata.
    head_sha      TEXT NOT NULL,
    state         TEXT NOT NULL DEFAULT 'pending'
                  CHECK (state IN ('pending', 'ready', 'failed')),
    -- [{title, kind, rationale, files:[{path, hunks:[{old_start, new_start,
    -- lines, explanation, moved_from}]}]}]. Validated on ingest; an unknown
    -- kind is stored as 'noise' so a newer agent vocabulary degrades quietly.
    groups        JSONB NOT NULL DEFAULT '[]',
    -- The diff exceeded the fetch caps, so the narrative covers only part of
    -- the change. Never an error: a partial walkthrough beats none.
    truncated     BOOLEAN NOT NULL DEFAULT false,
    omitted_files INT NOT NULL DEFAULT 0,
    -- The read-only run that produces the narrative. Also the fence that makes
    -- "a run for this head is already out" answerable without a second table.
    task_id       UUID,
    error         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
