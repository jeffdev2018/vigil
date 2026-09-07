-- At most one generation may be in flight per repo resource, enforced by the
-- database rather than by a check-then-insert.
--
-- This is what makes "a burst of merges produces one run" true: the post-merge
-- hook claims the building row with ON CONFLICT DO NOTHING and only dispatches
-- when the claim returns a row, so nine of ten near-simultaneous merges enqueue
-- nothing. The autopilot layer cannot provide this — its `concurrency_policy`
-- column was removed long ago (it never cancelled anything) and its idempotency
-- keys are scoped to a whole quota period, which would collapse every merge of
-- the month into one run instead of one per burst.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS code_wiki_snapshot_one_building_per_resource
    ON code_wiki_snapshot (project_resource_id) WHERE state = 'building';
