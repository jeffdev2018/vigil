CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_calendar_event_participant_person ON calendar_event_participant (workspace_id, participant_type, participant_id);
