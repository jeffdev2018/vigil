CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_mobile_push_token_user_token ON mobile_push_token (user_id, token);
