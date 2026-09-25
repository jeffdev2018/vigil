-- Inline approvals in chat: where one pending ask was posted, so the same
-- message can be rewritten in place when the ask settles instead of leaving a
-- live-looking button behind. One row per (ask, chat message). No foreign keys
-- (house rule); the installation, issue and ask are resolved in application
-- code, and the workspace delete statement sweeps this table explicitly.
--
-- source mirrors the approvals feed: 'decision', 'transition', 'goal_question'.
-- ask_id is the id that source settles by — issue_decision.id,
-- issue_transition_request.id, issue_goal.id.
CREATE TABLE channel_approval_message (
    id              UUID PRIMARY KEY,
    workspace_id    UUID NOT NULL,
    installation_id UUID NOT NULL,
    channel_type    TEXT NOT NULL,
    chat_id         TEXT NOT NULL,
    message_id      TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL,
    ask_id          UUID NOT NULL,
    settled         BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
