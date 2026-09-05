CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_org_structure_project_live
    ON org_structure (workspace_id, project_id) WHERE project_id IS NOT NULL AND status <> 'dissolved';
