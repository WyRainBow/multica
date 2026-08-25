-- The identity of one hook on one runtime, and the conflict target the
-- inventory upsert writes through. A re-scan of an unchanged machine must
-- update rows in place rather than pile up duplicates.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_runtime_hook_identity
    ON runtime_hook (workspace_id, runtime_id, provider, hook_name, event);
