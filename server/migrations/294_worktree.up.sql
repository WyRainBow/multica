-- A checkout on someone's machine, tracked as an object rather than inferred
-- from the cards worked inside it.
--
-- The ledger this supersedes read branch facts off issue metadata, one row per
-- card. That shape misses what is actually being managed: an integration or
-- launch branch usually has no card of its own, a worktree outlives the cards
-- worked in it, and there was nowhere to record that a tree is finished. Those
-- are properties of the checkout, so the checkout gets a row.
--
-- No foreign keys, per the repository rule. workspace_id and parent_id are
-- resolved in application code.
CREATE TABLE worktree (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    -- Hand-typed stable handle; what the CLI addresses a tree by. Unique per
    -- workspace, unlike path: a tree can be removed and a different one
    -- created at the same location later.
    name         TEXT NOT NULL,
    path         TEXT NOT NULL DEFAULT '',
    repo         TEXT NOT NULL DEFAULT '',
    branch       TEXT NOT NULL DEFAULT '',
    base_ref     TEXT NOT NULL DEFAULT '',
    -- Pipeline position, same vocabulary as the git.branch_role metadata key:
    -- what everything sits on (base) -> the work (feature) -> the batch
    -- carriers (integration, launch).
    role         TEXT NOT NULL DEFAULT 'feature'
                 CHECK (role IN ('base', 'feature', 'integration', 'launch')),
    status       TEXT NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active', 'blocked', 'merged', 'archived')),
    -- Machine facts, written by `multica worktree sync` from inside the
    -- checkout, never typed by hand. The 40-hex CHECK is the point: a merge
    -- claim in this table can always be re-verified against the repo, which a
    -- branch name alone could not be (branches move, get squashed, get
    -- deleted). Ported from agent-progress's verify.sh evidence gate.
    head_sha     TEXT NOT NULL DEFAULT ''
                 CHECK (head_sha = '' OR head_sha ~ '^[0-9a-f]{40}$'),
    merged_sha   TEXT NOT NULL DEFAULT ''
                 CHECK (merged_sha = '' OR merged_sha ~ '^[0-9a-f]{40}$'),
    merged_into  TEXT NOT NULL DEFAULT '',
    dirty        BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at  TIMESTAMPTZ,
    -- The navigation account: which session is driving this tree and what it
    -- waits on. One slot, edited in place. This is what the pinned per-issue
    -- session comment carried, minus the per-card copies that went stale.
    session_agent      TEXT NOT NULL DEFAULT '',
    session_resume     TEXT NOT NULL DEFAULT '',
    session_owner      TEXT NOT NULL DEFAULT '',
    next_action        TEXT NOT NULL DEFAULT '',
    session_updated_at TIMESTAMPTZ,
    -- The tree this one merges into: feature -> integration -> launch. Plain
    -- UUID; the cycle check and the tree walk live in application code.
    parent_id    UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
