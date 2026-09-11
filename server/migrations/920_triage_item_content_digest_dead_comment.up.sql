-- content_digest (516) was documented as a dedup fallback for sources with no
-- dedupe_key, but as defined it hashes normalized_title + payload: a matching
-- digest always implies a matching normalized_title, which
-- uq_triage_item_pending_title already folds before content_digest could add
-- anything. It was computed and stored but never read. The capture path (P3
-- audit, 2026-09) stopped writing it; the column stays for schema stability
-- (NOT NULL) but the comment must stop promising a fallback that cannot work
-- as defined.
COMMENT ON COLUMN triage_item.dedupe_key IS
    'Transport-level idempotency key (Idempotency-Key / X-GitHub-Delivery). '
    'Empty for unsigned senders; queue collapse then relies on '
    'normalized_title alone (see uq_triage_item_pending_title).';
COMMENT ON COLUMN triage_item.content_digest IS
    'Unused. Always empty: as defined (hash of normalized_title + payload) '
    'it cannot distinguish anything uq_triage_item_pending_title does not '
    'already fold on normalized_title alone. Kept NOT NULL for schema '
    'stability only.';
