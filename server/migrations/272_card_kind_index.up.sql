-- The tab filter reads (workspace_id, kind) and orders by the same key the
-- unfiltered list uses, so the index covers both the filtered page and the
-- distinct-kind read that builds the tabs.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_card_workspace_kind_created
    ON card (workspace_id, kind, created_at DESC, id DESC);
