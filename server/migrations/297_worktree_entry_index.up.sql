-- The only read: one tree's entries, newest first. Workspace-scoped so an entry
-- id from another tenant can never widen the scan.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_worktree_entry_tree_created
    ON worktree_entry (workspace_id, worktree_id, created_at DESC);
