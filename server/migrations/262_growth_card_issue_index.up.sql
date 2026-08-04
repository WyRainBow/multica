-- Powers "the cards written for this delivery" on the issue page. Partial:
-- cards with no issue are never looked up this way.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_growth_card_issue
    ON growth_card(issue_id) WHERE issue_id IS NOT NULL;
