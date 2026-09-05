CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_org_structure_default_live
    ON org_structure (workspace_id) WHERE project_id IS NULL AND status <> 'dissolved';
