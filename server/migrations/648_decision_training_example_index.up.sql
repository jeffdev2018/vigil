CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_decision_training_example_user_signature
    ON decision_training_example (workspace_id, user_id, signature, answered_at DESC);
