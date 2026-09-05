-- Meetings: a recorded conversation whose transcript is summarized into
-- triage items. No FK by repo rule; workspace deletion purges rows explicitly
-- (see DeleteWorkspace in queries/workspace.sql). Audio is never stored —
-- segments are transcribed then discarded, only the text lands here.
CREATE TABLE IF NOT EXISTS meeting (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    created_by    UUID NOT NULL,
    title         TEXT NOT NULL DEFAULT '',
    app_name      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'recording'
                  CHECK (status IN ('recording', 'summarizing', 'done', 'failed')),
    transcript    TEXT NOT NULL DEFAULT '',
    summary_md    TEXT NOT NULL DEFAULT '',
    segment_count INTEGER NOT NULL DEFAULT 0,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at      TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
