-- Format guard on issue.issue_type, mirroring the catalogue's own key CHECK.
-- The catalogue membership itself is validated in the application layer (there
-- is no foreign key by project rule); this only keeps an unparseable handle out
-- of the column.
--
-- NOT VALID keeps this an instant catalog change on a hot table; 797 validates.
-- Every existing row is NULL (793 added the column with no backfill), so the
-- scan cannot fail.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_issue_type_format;
ALTER TABLE issue ADD CONSTRAINT issue_issue_type_format
    CHECK (issue_type IS NULL OR issue_type ~ '^[a-z0-9][a-z0-9_]{0,31}$')
    NOT VALID;
