-- Twenty CRM integration (OS plan, chantier 2): one connection per workspace
-- to a Twenty instance. The API key and the webhook secret are sealed with
-- the server's Twenty secret box; the inbound token lives hashed on the
-- triage_source row of kind 'twenty' (same discipline as the email intake).
-- No foreign keys (house rule).
CREATE TABLE workspace_twenty_connection (
    id                    UUID PRIMARY KEY,
    workspace_id          UUID NOT NULL,
    base_url              TEXT NOT NULL,
    api_key_sealed        BYTEA NOT NULL,
    webhook_secret_sealed BYTEA,
    inbound_token_sealed  BYTEA,
    twenty_webhook_id     TEXT NOT NULL DEFAULT '',
    events                TEXT[] NOT NULL DEFAULT ARRAY['opportunity.*', 'person.created', 'company.created', 'task.created'],
    expose_to_agents      BOOLEAN NOT NULL DEFAULT true,
    status                TEXT NOT NULL DEFAULT 'connected' CHECK (status IN ('connected', 'error')),
    last_error            TEXT NOT NULL DEFAULT '',
    twenty_workspace_name TEXT NOT NULL DEFAULT '',
    created_by_id         UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
