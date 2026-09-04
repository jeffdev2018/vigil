-- Cross-provider self-review (K15): a review run is an ordinary run that
-- points at the run it reviews. No foreign key.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS review_of_task_id UUID;
