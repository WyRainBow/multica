-- name: CreateCard :one
INSERT INTO card (workspace_id, issue_id, author_type, author_id, title, content, kind)
VALUES ($1, sqlc.narg('issue_id'), $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetCard :one
-- Workspace-scoped so a card id from another tenant reads as not-found
-- rather than leaking a title.
SELECT * FROM card
WHERE id = $1 AND workspace_id = $2;

-- name: ListCards :many
-- Newest first — a card list is read as "what have I learned lately", not
-- as a backlog to work down. The keyset ordering matches
-- idx_card_workspace_created.
SELECT * FROM card
WHERE workspace_id = $1 AND NOT is_placeholder
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: CountCards :one
SELECT count(*) FROM card WHERE workspace_id = $1 AND NOT is_placeholder;

-- name: SearchCards :many
-- Cards are written to be found again months later, so a title-only match
-- would miss the body that holds the lesson. LOWER(col) LIKE rather than
-- ILIKE: the pg_bigm / pg_trgm GIN indexes this repo relies on for issue
-- search only match that form, and the pattern arrives already lowercased
-- from Go so SQL lowercases one side only.
SELECT * FROM card
WHERE workspace_id = sqlc.arg(workspace_id)
  AND NOT is_placeholder
  AND (LOWER(title) LIKE sqlc.arg(pattern) OR LOWER(content) LIKE sqlc.arg(pattern))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountSearchCards :one
-- The total has to describe the same set the page came from, or "showing 5 of
-- 13" reports the workspace rather than the search.
SELECT count(*) FROM card
WHERE workspace_id = sqlc.arg(workspace_id)
  AND NOT is_placeholder
  AND (LOWER(title) LIKE sqlc.arg(pattern) OR LOWER(content) LIKE sqlc.arg(pattern));

-- name: ListCardsForIssue :many
-- The cards written for one requirement, oldest first: read together they
-- are a narrative of how the work went, which reads forwards.
SELECT * FROM card
WHERE workspace_id = $1 AND issue_id = $2 AND NOT is_placeholder
ORDER BY created_at ASC, id ASC;

-- name: ListCardCountsForIssues :many
-- Badge counts for a set of issues in one round trip, so an issue list does
-- not fan out one query per row.
SELECT issue_id, count(*)::bigint AS card_count
FROM card
WHERE workspace_id = $1 AND issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
  AND NOT is_placeholder
GROUP BY issue_id;

-- name: UpdateCard :one
-- COALESCE on every field so a caller can patch one of them without
-- resending the rest.
UPDATE card SET
    title = COALESCE(sqlc.narg('title'), title),
    content = COALESCE(sqlc.narg('content'), content),
    kind = COALESCE(sqlc.narg('kind'), kind),
    -- An explicit edit is real content, so it promotes the row out of the
    -- placeholder set in the same statement that writes the text. Two steps
    -- (clear the flag, then save) would leave a window where the slot reads as
    -- occupied but empty; one statement has no window. Already-real cards are
    -- unaffected — FALSE is what they hold.
    is_placeholder = FALSE,
    issue_id = CASE WHEN sqlc.arg('clear_issue')::boolean THEN NULL
                    ELSE COALESCE(sqlc.narg('issue_id'), issue_id) END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteCard :exec
DELETE FROM card WHERE id = $1 AND workspace_id = $2;

-- name: ListCardsByKind :many
-- `kind` is a PATH, so selecting a folder takes everything below it: the exact
-- match, plus anything whose kind continues with a slash. The slash is what
-- keeps the boundary on a segment — a bare prefix would make `本地联调` swallow
-- `本地联调整理`. Matches the client's filterDocsByPath.
SELECT * FROM card
WHERE workspace_id = sqlc.arg(workspace_id)
  AND NOT is_placeholder
  AND (kind = sqlc.arg(kind) OR kind LIKE sqlc.arg(kind) || '/%')
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountCardsByKind :one
SELECT count(*) FROM card
WHERE workspace_id = sqlc.arg(workspace_id)
  AND NOT is_placeholder
  AND (kind = sqlc.arg(kind) OR kind LIKE sqlc.arg(kind) || '/%');

-- name: ListCardKinds :many
-- The tabs, in the order they should appear: most-used first, so a category
-- someone actually files into does not sit behind one they tried once. The
-- uncategorised bucket is excluded — "全部" already covers it, and a blank tab
-- label has nothing to render.
SELECT kind, count(*) AS card_count
FROM card
WHERE workspace_id = $1 AND kind <> '' AND NOT is_placeholder
GROUP BY kind
ORDER BY count(*) DESC, kind ASC;

-- name: ListIssueNamespaceCards :many
-- Every card filed under one issue, PLACEHOLDERS INCLUDED. The namespace view
-- is the one read that is supposed to see the empty slots; everything else
-- goes through ListCardsForIssue, which drops them.
SELECT * FROM card
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at ASC, id ASC;

-- name: CreateIssueNamespaceCard :one
-- The skeleton writer. Separate from CreateCard because that one is the public
-- write path and must never be able to mint a placeholder from a request body:
-- placeholder-ness is decided by the lifecycle, not by a caller.
INSERT INTO card (
    workspace_id, issue_id, author_type, author_id, title, content, kind, is_placeholder
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: PromotePlaceholderCard :one
-- Real content arriving at a slot a placeholder is holding: one statement
-- fills the row and clears the flag, so no reader can observe a slot that is
-- neither placeholder nor document. Deleting the placeholder and inserting a
-- document would open exactly that gap, and would change the card's id under
-- anyone already linking to it.
--
-- Authorship moves too: the placeholder was minted by whoever created the
-- issue, and the document belongs to whoever wrote it.
UPDATE card SET
    author_type = sqlc.arg('author_type'),
    author_id = sqlc.arg('author_id'),
    title = sqlc.arg('title'),
    content = sqlc.arg('content'),
    is_placeholder = FALSE,
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
  AND kind = sqlc.arg('kind')
  AND is_placeholder
RETURNING *;

-- name: DeleteIssuePlaceholderCards :exec
-- What a finished issue leaves behind. Only the slots still standing empty go;
-- anything promoted is a document now and is not touched by this.
DELETE FROM card
WHERE workspace_id = $1 AND issue_id = $2 AND is_placeholder;
