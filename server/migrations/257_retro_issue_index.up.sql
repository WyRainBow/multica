-- Powers "the retros written for this requirement" on the issue page.
-- Partial: retros with no issue are the minority and are never looked up
-- this way.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_retro_issue
    ON retro(issue_id) WHERE issue_id IS NOT NULL;
