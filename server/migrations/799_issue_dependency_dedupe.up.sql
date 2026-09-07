-- Duplicate (issue_id, depends_on_issue_id, type) rows exist because the
-- handler's pre-insert existence check is not atomic: two concurrent creates
-- both read "absent" and both insert. 800 closes that with a unique index, so
-- the duplicates have to go first — the index build would otherwise fail and
-- leave an INVALID index behind.
--
-- Keeping the OLDEST row of each group: the survivor is the edge whose id the
-- clients that already fetched the list are holding.
DELETE FROM issue_dependency d
USING issue_dependency keep
WHERE d.issue_id = keep.issue_id
  AND d.depends_on_issue_id = keep.depends_on_issue_id
  AND d.type = keep.type
  AND (d.created_at, d.id) > (keep.created_at, keep.id);
