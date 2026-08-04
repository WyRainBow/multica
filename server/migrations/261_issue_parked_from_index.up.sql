-- Reverse lookup: what was parked out of this requirement. Partial, because
-- only parked issues carry the column and they are a small minority.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_parked_from
    ON issue (workspace_id, parked_from_issue_id)
    WHERE parked_from_issue_id IS NOT NULL;
