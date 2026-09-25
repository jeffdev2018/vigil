-- Partial index for the deletion pass of the orphan sweep (JEF-292): it scans
-- only rows the mark pass has flagged, ordered by how long they have been
-- unreferenced.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_attachment_unreferenced_since
    ON attachment (unreferenced_since)
    WHERE unreferenced_since IS NOT NULL;
