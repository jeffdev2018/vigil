-- Issue router (K27): why a run landed where it did — risk level from the
-- project's blast radius rules, the pool chosen for it, and whether repeated
-- failures escalated it to a more capable pool.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS routing_decision JSONB;
