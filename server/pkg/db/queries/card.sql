-- name: CreateCard :one
INSERT INTO card (workspace_id, issue_id, author_type, author_id, title, content)
VALUES ($1, sqlc.narg('issue_id'), $2, $3, $4, $5)
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
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: CountCards :one
SELECT count(*) FROM card WHERE workspace_id = $1;

-- name: ListCardsForIssue :many
-- The cards written for one requirement, oldest first: read together they
-- are a narrative of how the work went, which reads forwards.
SELECT * FROM card
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at ASC, id ASC;

-- name: ListCardCountsForIssues :many
-- Badge counts for a set of issues in one round trip, so an issue list does
-- not fan out one query per row.
SELECT issue_id, count(*)::bigint AS card_count
FROM card
WHERE workspace_id = $1 AND issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
GROUP BY issue_id;

-- name: UpdateCard :one
-- COALESCE on every field so a caller can patch one of them without
-- resending the rest.
UPDATE card SET
    title = COALESCE(sqlc.narg('title'), title),
    content = COALESCE(sqlc.narg('content'), content),
    issue_id = CASE WHEN sqlc.arg('clear_issue')::boolean THEN NULL
                    ELSE COALESCE(sqlc.narg('issue_id'), issue_id) END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteCard :exec
DELETE FROM card WHERE id = $1 AND workspace_id = $2;
