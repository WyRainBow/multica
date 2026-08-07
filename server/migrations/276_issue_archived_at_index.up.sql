-- Every list query gains `archived_at IS NULL`; a partial index keeps that
-- predicate free and stays small because archived rows are the minority.
-- Single-statement file: CREATE INDEX CONCURRENTLY cannot run in a transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_unarchived
    ON issue(workspace_id) WHERE archived_at IS NULL;
