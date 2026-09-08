-- Linear Bridge (K21). Multica is installed as a Linear app: a Linear issue
-- assigned to the app's user is mirrored into a Multica issue owned by an
-- agent, and comments + status flow both ways from then on.
--
-- Three tables: the per-workspace installation (one Linear org, one Multica
-- agent, the sealed OAuth token and webhook secret), the issue link that makes
-- the mirror idempotent, and the comment link that stops a mirrored comment
-- from being echoed back to where it came from.
--
-- No foreign keys by repo convention; workspace teardown sweeps all three
-- explicitly (see PurgeWorkspaceLinear* in linear.sql).
CREATE TABLE linear_installation (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL,
    -- The Multica agent every mirrored issue is assigned to.
    agent_id                 UUID NOT NULL,
    linear_org_id            TEXT NOT NULL,
    linear_org_name          TEXT NOT NULL DEFAULT '',
    -- The Linear user id the app acts as. Two jobs: an issue assigned to it is
    -- what triggers a mirror, and an event actored by it is ours, so ignoring
    -- it is what breaks the echo loop.
    actor_user_id            TEXT NOT NULL DEFAULT '',
    access_token_encrypted   BYTEA NOT NULL,
    webhook_secret_encrypted BYTEA,
    linear_webhook_id        TEXT NOT NULL DEFAULT '',
    -- Linear workflow state type -> Multica status key.
    status_map               JSONB NOT NULL DEFAULT '{}',
    status                   TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'broken', 'revoked')),
    last_error               TEXT NOT NULL DEFAULT '',
    installed_by             UUID,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE linear_issue_link (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL,
    installation_id          UUID NOT NULL,
    issue_id                 UUID NOT NULL,
    linear_issue_id          TEXT NOT NULL,
    linear_issue_identifier  TEXT NOT NULL DEFAULT '',
    linear_team_id           TEXT NOT NULL DEFAULT '',
    linear_url               TEXT NOT NULL DEFAULT '',
    sync_state               TEXT NOT NULL DEFAULT 'active'
        CHECK (sync_state IN ('active', 'paused', 'broken')),
    last_synced_at           TIMESTAMPTZ,
    last_error               TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE linear_comment_link (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id   UUID NOT NULL,
    comment_id        UUID NOT NULL,
    linear_comment_id TEXT NOT NULL,
    -- 'in' = born in Linear, mirrored here. 'out' = born here, pushed there.
    -- Either way the row means "already delivered", so neither side re-sends.
    direction         TEXT NOT NULL CHECK (direction IN ('in', 'out')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
