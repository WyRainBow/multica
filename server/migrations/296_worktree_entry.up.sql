-- One line of what happened in a worktree.
--
-- Append-only by design: no UPDATE or DELETE path exists for it, here or in the
-- handler. A progress account that can be tidied afterwards is a snapshot, and
-- snapshots rot -- the reason this is worth keeping outside the card body is
-- exactly that a later round cannot quietly rewrite an earlier one. A wrong
-- line is corrected by adding the correction, the way a ledger works.
--
-- It exists because a git commit is too expensive a unit for "what did I change
-- this round". Committing per round of work is not realistic; losing the round
-- entirely is worse.
--
-- kind says what happened:
--   progress  a round of work, usually not committed yet
--   branch    a branch was created, rebased, or repointed
--   merge     merged somewhere; sha carries the merge commit
--   blocked   stopped, and on what
--   handoff   passed to another session or agent
--   verify    a check was run; body carries the command and its result
CREATE TABLE worktree_entry (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    worktree_id  UUID NOT NULL,
    -- The card this line belongs to, when it belongs to one. Plenty of entries
    -- are about the tree itself (rebase, merge, handoff) and carry no card.
    issue_id     UUID,
    kind         TEXT NOT NULL
                 CHECK (kind IN ('progress', 'branch', 'merge', 'blocked', 'handoff', 'verify')),
    body         TEXT NOT NULL,
    -- Same 40-hex gate as worktree: a commit reference in the ledger is either
    -- checkable or absent.
    sha          TEXT NOT NULL DEFAULT ''
                 CHECK (sha = '' OR sha ~ '^[0-9a-f]{40}$'),
    author_type  TEXT NOT NULL,
    author_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
