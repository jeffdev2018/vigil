-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE triage_item
    ADD CONSTRAINT triage_item_pkey PRIMARY KEY USING INDEX triage_item_pkey_uidx;
