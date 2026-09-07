-- F28: one held status change, waiting for an approver.
--
-- The issue is NOT moved when the request is filed: from_status/to_status
-- record the intent, the issue keeps its current status, and no run is
-- enqueued. Approving replays the move as the approver; rejecting either
-- leaves the issue alone or, when the rule names a reject_status_key, sends it
-- there.
--
-- rule_id is kept for the audit trail — which rule held this — and is
-- deliberately not re-read at decision time: a rule edited while a request was
-- pending must not silently change what the approver is deciding.
--
-- No FOREIGN KEY, per the repository rule. The unique index of 771 is what
-- actually enforces one pending request per issue; the 409 in the handler is
-- the friendly half of the same rule.
CREATE TABLE IF NOT EXISTS issue_transition_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    issue_id          UUID NOT NULL,
    from_status       TEXT NOT NULL,
    to_status         TEXT NOT NULL,
    rule_id           UUID,
    requested_by_type TEXT NOT NULL CHECK (requested_by_type IN ('member', 'agent')),
    requested_by_id   UUID NOT NULL,
    state             TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'approved', 'rejected', 'cancelled')),
    decided_by_type   TEXT,
    decided_by_id     UUID,
    decided_at        TIMESTAMPTZ,
    note              TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE issue_transition_request IS
    'F28: one status change held for approval. The issue is unchanged while state = pending. No FK by house rule.';
