-- One read-only calendar subscription per user per workspace: the ICS URL the
-- user pasted, plus the outcome of the last fetch so Settings can say why a
-- feed is silent instead of showing nothing.
--
-- Per workspace, not per user: the URL is a secret-bearing capability (an ICS
-- link is unauthenticated — anyone holding it reads the calendar), so a user
-- who shares a personal feed with one workspace has not shared it with every
-- workspace they belong to.
--
-- No foreign keys, per the repository rule: deleting a workspace or a user
-- deletes these rows explicitly in application code.
CREATE TABLE user_calendar_feed (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL,
    user_id         UUID NOT NULL,
    url             TEXT NOT NULL,
    last_fetched_at TIMESTAMPTZ,
    -- Empty means the last fetch succeeded; a message means it did not. Null
    -- means it has never been fetched.
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
