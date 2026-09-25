-- Orphaned attachment sweep (JEF-292): an attachment that no longer appears
-- in the markdown of its owning issue description or comment is marked with
-- the timestamp it became unreferenced. NULL means "still referenced" (or
-- never checked yet). A background sweeper (attachment_sweep.go) sets and
-- clears this column, then deletes rows that have stayed unreferenced past
-- a retention window.
ALTER TABLE attachment ADD COLUMN unreferenced_since TIMESTAMPTZ NULL;

COMMENT ON COLUMN attachment.unreferenced_since IS
    'When the sweep first found this attachment absent from its owning content; NULL while referenced.';
