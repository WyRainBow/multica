-- The CLI addresses a tree by name inside a workspace, so the name has to
-- resolve to exactly one row. Doubles as the workspace-scoped list index.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_worktree_workspace_name
    ON worktree (workspace_id, name);
