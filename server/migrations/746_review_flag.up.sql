-- Review flags by severity (F06 / JEF-19). A structured finding an agent
-- records during its run: a file, a line range of one revision of a pull
-- request's head, a severity and a confidence. The reviewer reads a sorted
-- list of "what is wrong and how sure are we" instead of scrolling prose.
--
-- Anchored to (pr_id, head_sha), not to the pull request: a flag describes a
-- line range of ONE revision. When the head moves the flag is marked `stale`
-- rather than deleted — the finding was true of the code it was written
-- against, and a reviewer who has to explain a decision needs that record.
--
-- No foreign keys by repository convention: workspace teardown deletes these
-- rows explicitly (purge step in handler/workspace.go).
CREATE TABLE IF NOT EXISTS review_flag (
    id            UUID PRIMARY KEY,
    workspace_id  UUID NOT NULL,
    -- The issue the pull request is linked to. The flag is read from the
    -- issue panel and authorized through the issue, so it is stored on it.
    issue_id      UUID NOT NULL,
    -- Which table pr_id points at: github_pull_request or vcs_pull_request.
    pr_source     TEXT NOT NULL CHECK (pr_source IN ('github', 'vcs')),
    pr_id         UUID NOT NULL,
    -- The commit the line range belongs to. Part of the flag's meaning, not
    -- metadata: the same line number means something else on another head.
    head_sha      TEXT NOT NULL,
    file_path     TEXT NOT NULL,
    line_start    INT NOT NULL,
    line_end      INT NOT NULL,
    -- Which side of the diff the range is on. 'new' is the changed code;
    -- 'old' lets a flag point at a deletion.
    side          TEXT NOT NULL DEFAULT 'new' CHECK (side IN ('old', 'new')),
    -- Sorted bug -> warning -> info. An unknown value never reaches here:
    -- the API maps it to 'info' so a newer agent vocabulary degrades to
    -- "worth reading" rather than to "must fix".
    severity      TEXT NOT NULL CHECK (severity IN ('bug', 'warning', 'info')),
    -- How sure the author is, 0-100. NULL means "did not say", which sorts
    -- last within a severity rather than as zero.
    confidence    SMALLINT CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 100)),
    title         TEXT NOT NULL,
    body          TEXT NOT NULL DEFAULT '',
    -- Exactly one of these is set: a run stamps the agent, a human the user.
    author_agent_id UUID,
    author_user_id  UUID,
    -- The run that recorded it, when an agent did. Also what the per-task cap
    -- counts, so one run cannot bury the reviewer under its own findings.
    task_id       UUID,
    -- Reserved for the anchored discussion thread (F07). Nothing writes it yet.
    comment_id    UUID,
    state         TEXT NOT NULL DEFAULT 'open'
                  CHECK (state IN ('open', 'resolved', 'dismissed', 'stale')),
    -- Who settled it. Humans only, so 'member' today; the column is typed
    -- rather than a bare id because the resolver is displayed next to it.
    resolved_by_type TEXT NOT NULL DEFAULT '',
    resolved_by_id   UUID,
    resolved_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
