-- Morning briefing (K30): one row per workspace and local date, so a retried
-- job or a manual trigger never sends the day's briefing twice. The
-- briefing itself is recomposed from live data; only the send is recorded.
CREATE TABLE IF NOT EXISTS morning_briefing_sent (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID NOT NULL,
    sent_for_date      DATE NOT NULL,
    channels_delivered JSONB NOT NULL DEFAULT '[]',
    summary            JSONB NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
