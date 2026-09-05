CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_campaign_shard_campaign_status ON campaign_shard (refactor_campaign_id, merge_status);
