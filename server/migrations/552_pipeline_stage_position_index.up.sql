CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_pipeline_stage_position ON pipeline_stage (pipeline_id, position);
