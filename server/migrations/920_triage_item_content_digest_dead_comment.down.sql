COMMENT ON COLUMN triage_item.dedupe_key IS
    'Transport-level idempotency key (Idempotency-Key / X-GitHub-Delivery). '
    'Empty for unsigned senders; content_digest is the fallback then.';
COMMENT ON COLUMN triage_item.content_digest IS NULL;
