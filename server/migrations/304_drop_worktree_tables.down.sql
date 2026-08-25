-- Recreates the shape as it stood at 298, not the rows.
--
-- The rows cannot come back from here. They live in agent-progress as YAML now,
-- and a data-only dump taken at drop time is the other copy — a rollback that
-- needs the data restores from one of those two, not from this file.
--
-- The two indexes (295 idx_worktree_workspace_name, 297
-- idx_worktree_entry_tree_created) are deliberately NOT recreated here. Every
-- index in this repository must be built CONCURRENTLY, and a concurrent build
-- cannot share a file with other statements. Re-running 295 and 297 is how they
-- come back; a table with no rows does not need them in the meantime.
CREATE TABLE worktree (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name         TEXT NOT NULL,
    path         TEXT NOT NULL DEFAULT '',
    repo         TEXT NOT NULL DEFAULT '',
    branch       TEXT NOT NULL DEFAULT '',
    base_ref     TEXT NOT NULL DEFAULT '',
    -- Vocabulary as 298 left it: 'launch' became 'release', 'hotfix' was added.
    role         TEXT NOT NULL DEFAULT 'feature'
                 CONSTRAINT worktree_role_check
                 CHECK (role IN ('base', 'feature', 'integration', 'release', 'hotfix')),
    status       TEXT NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active', 'blocked', 'merged', 'archived')),
    -- The 40-hex gate is the point: a merge claim here can always be
    -- re-verified against the repo, which a branch name could not be.
    head_sha     TEXT NOT NULL DEFAULT ''
                 CHECK (head_sha = '' OR head_sha ~ '^[0-9a-f]{40}$'),
    merged_sha   TEXT NOT NULL DEFAULT ''
                 CHECK (merged_sha = '' OR merged_sha ~ '^[0-9a-f]{40}$'),
    merged_into  TEXT NOT NULL DEFAULT '',
    dirty        BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at  TIMESTAMPTZ,
    session_agent      TEXT NOT NULL DEFAULT '',
    session_resume     TEXT NOT NULL DEFAULT '',
    session_owner      TEXT NOT NULL DEFAULT '',
    next_action        TEXT NOT NULL DEFAULT '',
    session_updated_at TIMESTAMPTZ,
    parent_id    UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE worktree_entry (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    worktree_id  UUID NOT NULL,
    issue_id     UUID,
    kind         TEXT NOT NULL
                 CHECK (kind IN ('progress', 'branch', 'merge', 'blocked', 'handoff', 'verify')),
    body         TEXT NOT NULL,
    sha          TEXT NOT NULL DEFAULT ''
                 CHECK (sha = '' OR sha ~ '^[0-9a-f]{40}$'),
    author_type  TEXT NOT NULL,
    author_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
