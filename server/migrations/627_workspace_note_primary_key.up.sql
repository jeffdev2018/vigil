-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE workspace_note
    ADD CONSTRAINT workspace_note_pkey PRIMARY KEY USING INDEX workspace_note_pkey_uidx;
