-- name: ListWorktrees :many
-- Pipeline order, not creation order: base sits under everything, features are
-- the work, integration and launch carry batches. Reading a ledger back in the
-- order rows happened to be typed hides that structure.
SELECT * FROM worktree
WHERE workspace_id = $1
ORDER BY
    CASE role
        WHEN 'base' THEN 0
        WHEN 'feature' THEN 1
        WHEN 'integration' THEN 2
        WHEN 'launch' THEN 3
        ELSE 4
    END,
    name ASC;

-- name: GetWorktree :one
SELECT * FROM worktree
WHERE id = $1 AND workspace_id = $2;

-- name: GetWorktreeByName :one
-- The CLI addresses trees by name; the unique index makes this single-row.
SELECT * FROM worktree
WHERE workspace_id = $1 AND name = $2;

-- name: CreateWorktree :one
INSERT INTO worktree (
    workspace_id, name, path, repo, branch, base_ref, role, status, parent_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateWorktree :one
-- COALESCE per field so a caller can repoint the base without resending the
-- path, or close a tree without resending anything else.
UPDATE worktree SET
    name      = COALESCE(sqlc.narg('name'), name),
    path      = COALESCE(sqlc.narg('path'), path),
    repo      = COALESCE(sqlc.narg('repo'), repo),
    branch    = COALESCE(sqlc.narg('branch'), branch),
    base_ref  = COALESCE(sqlc.narg('base_ref'), base_ref),
    role      = COALESCE(sqlc.narg('role'), role),
    status    = COALESCE(sqlc.narg('status'), status),
    parent_id = CASE
        WHEN sqlc.arg('clear_parent')::bool THEN NULL
        ELSE COALESCE(sqlc.narg('parent_id'), parent_id)
    END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: UpdateWorktreeSession :one
-- The navigation slot. Separate from UpdateWorktree because it is written by a
-- different actor at a different rhythm: a session claims the tree, the branch
-- facts do not change when it does.
UPDATE worktree SET
    session_agent  = COALESCE(sqlc.narg('session_agent'), session_agent),
    session_resume = COALESCE(sqlc.narg('session_resume'), session_resume),
    session_owner  = COALESCE(sqlc.narg('session_owner'), session_owner),
    next_action    = COALESCE(sqlc.narg('next_action'), next_action),
    session_updated_at = now(),
    updated_at         = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: SyncWorktreeFacts :one
-- Machine facts only, written from inside the checkout. verified_at stamps when
-- the repo last actually said so, which is what makes a stale row visible
-- instead of merely wrong.
UPDATE worktree SET
    branch      = COALESCE(sqlc.narg('branch'), branch),
    head_sha    = COALESCE(sqlc.narg('head_sha'), head_sha),
    merged_sha  = COALESCE(sqlc.narg('merged_sha'), merged_sha),
    merged_into = COALESCE(sqlc.narg('merged_into'), merged_into),
    dirty       = COALESCE(sqlc.narg('dirty'), dirty),
    status      = COALESCE(sqlc.narg('status'), status),
    verified_at = now(),
    updated_at  = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteWorktree :exec
DELETE FROM worktree WHERE id = $1 AND workspace_id = $2;

-- name: DeleteWorktreeEntriesForWorktree :exec
-- Dependent cleanup, run in the same transaction as DeleteWorktree: there is no
-- cascading delete in this schema, so the parent operation owns it.
DELETE FROM worktree_entry WHERE worktree_id = $1 AND workspace_id = $2;

-- name: ListWorktreeEntries :many
-- Newest first: a ledger is read from the top, and the window that matters is
-- "since the last merge", not the whole history.
SELECT * FROM worktree_entry
WHERE workspace_id = $1 AND worktree_id = $2
ORDER BY created_at DESC, id DESC
LIMIT $3;

-- name: ListRecentWorktreeEntries :many
-- The workspace-wide feed the ledger page opens with: what moved lately,
-- across every tree.
SELECT * FROM worktree_entry
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: CreateWorktreeEntry :one
INSERT INTO worktree_entry (
    workspace_id, worktree_id, issue_id, kind, body, sha, author_type, author_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CountWorktreeEntries :many
-- One query for the whole list rather than one per tree, so the ledger can show
-- a count per row without N round trips.
SELECT worktree_id, count(*) AS entry_count
FROM worktree_entry
WHERE workspace_id = $1
GROUP BY worktree_id;
