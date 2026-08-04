-- name: CreateRetro :one
INSERT INTO retro (workspace_id, issue_id, author_type, author_id, title, content)
VALUES ($1, sqlc.narg('issue_id'), $2, $3, $4, $5)
RETURNING *;

-- name: GetRetro :one
-- Workspace-scoped so a retro id from another tenant reads as not-found
-- rather than leaking a title.
SELECT * FROM retro
WHERE id = $1 AND workspace_id = $2;

-- name: ListRetros :many
-- Newest first — a retro list is read as "what have I learned lately", not
-- as a backlog to work down. The keyset ordering matches
-- idx_retro_workspace_created.
SELECT * FROM retro
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: CountRetros :one
SELECT count(*) FROM retro WHERE workspace_id = $1;

-- name: ListRetrosForIssue :many
-- The retros written for one requirement, oldest first: read together they
-- are a narrative of how the work went, which reads forwards.
SELECT * FROM retro
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at ASC, id ASC;

-- name: ListRetroCountsForIssues :many
-- Badge counts for a set of issues in one round trip, so an issue list does
-- not fan out one query per row.
SELECT issue_id, count(*)::bigint AS retro_count
FROM retro
WHERE workspace_id = $1 AND issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
GROUP BY issue_id;

-- name: UpdateRetro :one
-- COALESCE on every field so a caller can patch one of them without
-- resending the rest.
UPDATE retro SET
    title = COALESCE(sqlc.narg('title'), title),
    content = COALESCE(sqlc.narg('content'), content),
    issue_id = CASE WHEN sqlc.arg('clear_issue')::boolean THEN NULL
                    ELSE COALESCE(sqlc.narg('issue_id'), issue_id) END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteRetro :exec
DELETE FROM retro WHERE id = $1 AND workspace_id = $2;
