-- name: ListIssueResources :many
-- Track order, not creation order: the list is arranged by hand, and reading it
-- back the way it was typed would undo that.
SELECT * FROM issue_resource
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY position ASC, created_at ASC;

-- name: GetIssueResource :one
SELECT * FROM issue_resource
WHERE id = $1 AND workspace_id = $2;

-- name: MaxIssueResourcePosition :one
-- Where the next row goes. COALESCE so the first resource on an issue starts
-- the sequence rather than returning no row.
SELECT COALESCE(MAX(position), -1)::int AS max_position
FROM issue_resource
WHERE workspace_id = $1 AND issue_id = $2;

-- name: CreateIssueResource :one
INSERT INTO issue_resource (workspace_id, issue_id, url, title, author_type, author_id, position)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateIssueResource :one
-- COALESCE on both fields so a caller can rename a resource without resending
-- the URL, or repoint it without resending the title.
UPDATE issue_resource SET
    url = COALESCE(sqlc.narg('url'), url),
    title = COALESCE(sqlc.narg('title'), title),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssueResource :exec
DELETE FROM issue_resource WHERE id = $1 AND workspace_id = $2;

-- name: CountIssueResourcesForIssues :many
-- One query for a list of issues rather than one per issue, so a board or a
-- sub-issue list can show a count without N round trips.
SELECT issue_id, count(*) AS resource_count
FROM issue_resource
WHERE workspace_id = $1 AND issue_id = ANY($2::uuid[])
GROUP BY issue_id;
