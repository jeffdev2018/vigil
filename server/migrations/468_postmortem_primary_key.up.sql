-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE postmortem
    ADD CONSTRAINT postmortem_pkey PRIMARY KEY USING INDEX postmortem_pkey_uidx;
