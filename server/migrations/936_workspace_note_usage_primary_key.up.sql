-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE workspace_note_usage
    ADD CONSTRAINT workspace_note_usage_pkey PRIMARY KEY USING INDEX workspace_note_usage_pkey_uidx;
