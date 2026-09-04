-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE triage_source
    ADD CONSTRAINT triage_source_pkey PRIMARY KEY USING INDEX triage_source_pkey_uidx;
