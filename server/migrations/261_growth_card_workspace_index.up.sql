-- The card list reads newest-first within a workspace.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_growth_card_workspace_created
    ON growth_card(workspace_id, created_at DESC, id DESC);
