-- The list page reads newest-first within a workspace; this is that query.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_retro_workspace_created
    ON retro(workspace_id, created_at DESC, id DESC);
