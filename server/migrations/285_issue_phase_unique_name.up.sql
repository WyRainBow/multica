-- One station per name on an issue. A route with two stations called 实施中
-- cannot say which one a comment belongs to, and the count on each becomes
-- meaningless.
--
-- Unique on lower(name) so "开始" and " 开始 " cannot both exist — the handler
-- trims and compares case-insensitively, and this makes that the database's
-- rule rather than a convention the next caller might not follow.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_phase_unique_name
    ON issue_phase (issue_id, lower(name));
