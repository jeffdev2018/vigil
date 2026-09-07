-- A cycle is pinnable to the sidebar like an issue, a project or a view
-- (F29). Widened only, so 791 can validate without a row failing.
ALTER TABLE pinned_item DROP CONSTRAINT IF EXISTS pinned_item_item_type_check;
ALTER TABLE pinned_item ADD CONSTRAINT pinned_item_item_type_check
    CHECK (item_type IN ('issue', 'project', 'view', 'cycle'))
    NOT VALID;
