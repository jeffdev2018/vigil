CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_calendar_event_participant_who ON calendar_event_participant (event_id, participant_type, participant_id);
