-- name: CreateIssuePhase :one
INSERT INTO issue_phase (workspace_id, issue_id, name, position)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListIssuePhases :many
-- Track order, not creation order: the stations are a route, and reading them
-- out of order says nothing.
SELECT * FROM issue_phase
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY position ASC, created_at ASC;

-- name: GetIssuePhase :one
-- Workspace-scoped so a phase id from another tenant reads as not-found.
SELECT * FROM issue_phase
WHERE id = $1 AND workspace_id = $2;

-- name: MaxIssuePhasePosition :one
-- Where the next station goes when the caller does not say. COALESCE so the
-- first phase on an issue starts the sequence rather than returning no row.
SELECT COALESCE(MAX(position), -1)::int AS max_position
FROM issue_phase
WHERE workspace_id = $1 AND issue_id = $2;

-- name: EnterIssuePhase :one
-- Mark arrival. `entered_at` is derived here rather than accepted from the
-- caller — a timestamp someone has to remember to type is a timestamp that is
-- wrong. Borrowed from Plane's `_sync_completed_at`.
--
-- Idempotent: re-entering a phase already entered keeps the ORIGINAL arrival
-- time, because "when did we get here" has one answer even if the issue
-- bounced back later. Re-entering also clears completion — the station is
-- current again.
UPDATE issue_phase
SET entered_at   = COALESCE(entered_at, now()),
    completed_at = NULL,
    updated_at   = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CompleteIssuePhase :one
-- Mark departure. Refuses a phase never entered: completing something that
-- never started would record a route the work did not take.
UPDATE issue_phase
SET completed_at = now(),
    updated_at   = now()
WHERE id = $1 AND workspace_id = $2 AND entered_at IS NOT NULL
RETURNING *;

-- name: UpdateIssuePhase :one
UPDATE issue_phase
SET name       = COALESCE(sqlc.narg('name'), name),
    position   = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssuePhase :exec
-- Its comments are deleted first, in application code. A phase is a
-- container: removing it removes what it held.
DELETE FROM issue_phase
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteCommentsInPhase :exec
-- Deletes the comments filed under a phase AND their replies.
--
-- Replies are included because they carry no phase of their own — a thread is
-- filed by its root — so leaving them would strand answers whose question is
-- gone. Written as one statement rather than a read-then-delete so a reply
-- arriving between the two cannot survive its root.
DELETE FROM comment AS target
WHERE target.workspace_id = $2
  AND (
    target.phase_id = $1
    OR target.parent_id IN (
      SELECT root.id FROM comment AS root
      WHERE root.phase_id = $1 AND root.workspace_id = $2
    )
  );

-- name: SetCommentPhase :one
UPDATE comment
SET phase_id = sqlc.narg('phase_id'), updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: CountCommentsPerPhase :many
-- How much each station holds, for the list view. Only phases that have
-- comments come back; the caller fills zeros.
SELECT phase_id, COUNT(*)::bigint AS comment_count
FROM comment
WHERE workspace_id = $1 AND issue_id = $2 AND phase_id IS NOT NULL
GROUP BY phase_id;
