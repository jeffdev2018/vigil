-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE project_review_config
    ADD CONSTRAINT project_review_config_pkey PRIMARY KEY USING INDEX project_review_config_pkey_uidx;
