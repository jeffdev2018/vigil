-- NOT VALID keeps this an instant catalog change; every existing row already
-- satisfies it (987's default is 'fact'). 989 validates it separately under
-- a lock that allows normal reads/writes.
ALTER TABLE workspace_note ADD CONSTRAINT workspace_note_kind_check
    CHECK (kind IN ('fact', 'decision', 'procedure', 'glossary', 'episode'))
    NOT VALID;
