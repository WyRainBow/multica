-- Comments grouped under a phase. Partial: most comments carry no phase.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_phase
    ON comment (phase_id)
    WHERE phase_id IS NOT NULL;
