-- F12 (JEF-10): the dev server one run started in its worktree, and where a
-- reviewer can reach it.
--
-- One row per task: a run has at most one preview, so the unique index of 779
-- is the whole concurrency story. The row is created by the daemon after it
-- probed the port, and dies with the run — a preview that outlives its worktree
-- would point at a directory that no longer exists.
--
-- scheme says HOW the URL reaches the server, and it is the one field the UI
-- must never guess: `relay` means the server can proxy the request down the
-- daemon's existing WebSocket, `loopback` means it cannot and the address is
-- only meaningful on the machine that ran it. A daemon too old to relay, or a
-- self-hosted deployment that turned the relay off, produces `loopback`.
--
-- No FOREIGN KEY, per the repository rule. workspace_id / task_id / runtime_id
-- are validated in application code and removed with the workspace.
CREATE TABLE IF NOT EXISTS run_preview (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    task_id          UUID NOT NULL,
    runtime_id       UUID NOT NULL,
    port             INT  NOT NULL,
    scheme           TEXT NOT NULL CHECK (scheme IN ('relay', 'loopback')),
    status           TEXT NOT NULL CHECK (status IN ('starting', 'ready', 'stale', 'stopped', 'error')),
    health_path      TEXT NOT NULL DEFAULT '/',
    error            TEXT,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stopped_at       TIMESTAMPTZ
);

COMMENT ON TABLE run_preview IS
    'F12: the dev server a run started in its worktree. One row per task (779). No FK by house rule.';
