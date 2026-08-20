CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_delivery_receipt_issue_created_idx
    ON issue_delivery_receipt (issue_id, created_at DESC);
