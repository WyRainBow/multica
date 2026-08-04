-- The list query: every card in a workspace, newest first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_card_workspace_created
    ON card (workspace_id, created_at DESC);
