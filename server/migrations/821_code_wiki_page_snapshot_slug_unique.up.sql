CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS code_wiki_page_snapshot_slug_key
    ON code_wiki_page (snapshot_id, slug);
