CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_campaign_shard_position ON campaign_shard (refactor_campaign_id, merge_position);
