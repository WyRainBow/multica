-- Partial: only pinned rows are ever looked up this way, and they are a
-- handful per issue against a table that holds every comment ever written.
-- CONCURRENTLY and alone in its own file — PostgreSQL rejects a concurrent
-- build inside a transaction or a multi-statement string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_pinned
    ON comment (issue_id, pinned_at DESC)
    WHERE pinned_at IS NOT NULL;
