-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE project_sandbox_policy
    ADD CONSTRAINT project_sandbox_policy_pkey PRIMARY KEY USING INDEX project_sandbox_policy_pkey_uidx;
