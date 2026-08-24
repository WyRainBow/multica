-- Partial index: the only queries that look for placeholders look for them
-- under one issue — the namespace view, the promote-in-place write, and the
-- cleanup a terminal status runs. Placeholders are a small minority of the
-- table and the predicate keeps the index that size, while every ordinary card
-- read stays on idx_card_workspace_created / idx_card_workspace_kind_created
-- and pays nothing for this one.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_card_issue_placeholder
    ON card (issue_id) WHERE is_placeholder;
