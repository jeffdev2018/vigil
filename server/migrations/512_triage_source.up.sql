-- Triage M1: named inbound issue sources. One row per (workspace, kind,
-- source object) — e.g. the autopilot whose webhook trigger admits external
-- payloads. ref_id is resolved in application code (no FK by repo rule);
-- triage_item references this table logically through source_id.
CREATE TABLE triage_source (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN (
        'autopilot_webhook',
        'autopilot_schedule',
        'channel',
        'agent_create',
        'quick_create'
    )),
    ref_id UUID NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    icon TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'direct' CHECK (mode IN ('gate', 'direct', 'blocked')),
    auto_accept JSONB NOT NULL DEFAULT '{}'::jsonb,
    cap_per_hour INTEGER NOT NULL DEFAULT 0,
    expiry_days INTEGER NOT NULL DEFAULT 14,
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
