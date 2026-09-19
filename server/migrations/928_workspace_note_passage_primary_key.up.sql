-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE workspace_note_passage
    ADD CONSTRAINT workspace_note_passage_pkey PRIMARY KEY USING INDEX workspace_note_passage_pkey_uidx;
