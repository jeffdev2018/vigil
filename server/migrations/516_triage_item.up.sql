-- Triage M1: inbound issue material awaiting admission. An item is NOT an
-- issue row: it precedes AllocateIssueNumber, so unaccepted inbound work
-- consumes no issue quota and never appears in issue counts, rollups, or the
-- duplicate guard. Accepting an item (M2) is the only path that creates the
-- issue row, reusing the item's origin_type/origin_id.
--
-- In M1 (shadow mode) every captured item has shadow=true and the routing
-- behavior is unchanged; the rows exist only to measure what gating would
-- hold. dropped rows record webhook deliveries that produced no issue
-- (issue limit reached, recent duplicate, ...) — the silent-loss population.
CREATE TABLE triage_item (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    source_id UUID NOT NULL,
    origin_type TEXT NOT NULL,
    origin_id UUID,
    actor_type TEXT CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID,
    dedupe_key TEXT NOT NULL DEFAULT '',
    content_digest TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    normalized_title TEXT NOT NULL DEFAULT '',
    body_markdown TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN (
        'pending', 'accepted', 'dismissed', 'merged', 'superseded', 'expired', 'dropped'
    )),
    drop_reason TEXT,
    resolution_reason TEXT,
    collapse_count INTEGER NOT NULL DEFAULT 1,
    verdict JSONB,
    verdict_agent_id UUID,
    verdict_at TIMESTAMPTZ,
    verdict_revision BIGINT NOT NULL DEFAULT 0,
    issue_id UUID,
    duplicate_of_issue_id UUID,
    replaced_by_item_id UUID,
    shadow BOOLEAN NOT NULL DEFAULT FALSE,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    resolved_by_type TEXT CHECK (resolved_by_type IN ('member', 'agent', 'system')),
    resolved_by_id UUID,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (state = 'pending' AND issue_id IS NULL AND resolved_at IS NULL)
        OR (state = 'accepted' AND issue_id IS NOT NULL AND resolved_at IS NOT NULL)
        OR (state = 'merged' AND issue_id IS NULL AND duplicate_of_issue_id IS NOT NULL AND resolved_at IS NOT NULL)
        OR (state IN ('dismissed', 'superseded', 'expired') AND issue_id IS NULL AND resolved_at IS NOT NULL)
        OR (state = 'dropped' AND issue_id IS NULL AND drop_reason IS NOT NULL AND resolved_at IS NULL)
    ),
    CHECK (jsonb_typeof(payload) = 'object'),
    CHECK (pg_column_size(payload) <= 32768),
    CHECK (verdict IS NULL OR jsonb_typeof(verdict) = 'object'),
    CHECK (collapse_count >= 1)
);

COMMENT ON COLUMN triage_item.dedupe_key IS
    'Transport-level idempotency key (Idempotency-Key / X-GitHub-Delivery). '
    'Empty for unsigned senders; content_digest is the fallback then.';
COMMENT ON COLUMN triage_item.normalized_title IS
    'lower(btrim(regexp_replace(title, ''[[:space:]]+'', '' '', ''g''))) — the same '
    'normalization issueguard uses, so queue collapse and issue duplicate '
    'detection agree.';
COMMENT ON COLUMN triage_item.shadow IS
    'true = captured for measurement while the source is still routed direct; '
    'never shown in the triage queue.';
