ALTER TABLE worktree DROP CONSTRAINT IF EXISTS worktree_role_check;
UPDATE worktree SET role = 'feature' WHERE role = 'hotfix';
UPDATE worktree SET role = 'launch' WHERE role = 'release';
ALTER TABLE worktree ADD CONSTRAINT worktree_role_check
    CHECK (role IN ('base', 'feature', 'integration', 'launch'));
