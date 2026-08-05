-- The list query: one issue's phases, in track order.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_phase_issue
    ON issue_phase (workspace_id, issue_id, position);
