-- Branch roles follow the vocabulary the branches themselves use:
-- feature/<card>, integration/<topic>, release/<date>, hotfix/<desc>, on top of
-- whatever the base branch is. The table shipped with 'launch' where the naming
-- rule says 'release', and had no room for a hotfix at all — a role a tree
-- cannot be given is a role that gets recorded as something else.
ALTER TABLE worktree DROP CONSTRAINT IF EXISTS worktree_role_check;

UPDATE worktree SET role = 'release' WHERE role = 'launch';

ALTER TABLE worktree ADD CONSTRAINT worktree_role_check
    CHECK (role IN ('base', 'feature', 'integration', 'release', 'hotfix'));
