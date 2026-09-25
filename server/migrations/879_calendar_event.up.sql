-- Native calendar (OS plan, chantier 19): events with participants who are
-- members or agents, proposals an agent files and a person accepts, a link
-- to the issue or project the event serves. No foreign keys (house rule):
-- issue, project, participants and the proposal's decision card are
-- resolved in application code; the workspace delete statement sweeps
-- these tables explicitly.
CREATE TABLE calendar_event (
    id              UUID PRIMARY KEY,
    workspace_id    UUID NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    starts_at       TIMESTAMPTZ NOT NULL,
    ends_at         TIMESTAMPTZ NOT NULL,
    all_day         BOOLEAN NOT NULL DEFAULT false,
    timezone        TEXT NOT NULL DEFAULT 'UTC',
    location        TEXT NOT NULL DEFAULT '',
    issue_id        UUID,
    project_id      UUID,
    status          TEXT NOT NULL DEFAULT 'scheduled' CHECK (status IN ('proposed', 'scheduled', 'cancelled')),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id   UUID NOT NULL,
    source          TEXT NOT NULL DEFAULT 'vigil',
    external_id     TEXT NOT NULL DEFAULT '',
    decision_id     UUID,
    reminded_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at)
);

CREATE TABLE calendar_event_participant (
    id               UUID PRIMARY KEY,
    workspace_id     UUID NOT NULL,
    event_id         UUID NOT NULL,
    participant_type TEXT NOT NULL CHECK (participant_type IN ('member', 'agent')),
    participant_id   UUID NOT NULL,
    response         TEXT NOT NULL DEFAULT 'pending' CHECK (response IN ('pending', 'accepted', 'declined', 'tentative')),
    required         BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The outbound ICS feed: one token per (workspace, member), stored as its
-- sha256 digest; the clear token is in the URL handed out once.
CREATE TABLE calendar_feed_token (
    id           UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    user_id      UUID NOT NULL,
    token_hash   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
