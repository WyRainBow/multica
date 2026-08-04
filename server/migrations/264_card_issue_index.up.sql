-- Cards written about one requirement. Partial: most cards have no issue.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_card_issue
    ON card (workspace_id, issue_id)
    WHERE issue_id IS NOT NULL;
