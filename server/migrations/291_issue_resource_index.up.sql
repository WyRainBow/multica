-- The only read: every resource on one issue, in track order. Covers the
-- workspace scope the query always carries, so a resource id from another
-- tenant can never widen the scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_resource_issue_position
    ON issue_resource (workspace_id, issue_id, position, created_at);
