-- name: CreateGrowthCard :one
INSERT INTO growth_card (
    workspace_id, issue_id, author_type, author_id,
    title, systems, unknowns, agent_plan, understood, verified, learned, next_gaps
) VALUES (
    $1, sqlc.narg('issue_id'), $2, $3,
    $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetGrowthCard :one
-- Workspace-scoped so a card id from another tenant reads as not-found rather
-- than leaking its contents.
SELECT * FROM growth_card
WHERE id = $1 AND workspace_id = $2;

-- name: ListGrowthCards :many
-- Newest first — the list answers "what have I learned lately", not "what is
-- left to do". Matches idx_growth_card_workspace_created.
SELECT * FROM growth_card
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: CountGrowthCards :one
SELECT count(*) FROM growth_card WHERE workspace_id = $1;

-- name: ListGrowthCardsForIssue :many
-- Oldest first: read together, one delivery's cards are a narrative of how the
-- work went, and a narrative reads forwards.
SELECT * FROM growth_card
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY created_at ASC, id ASC;

-- name: UpdateGrowthCard :one
-- COALESCE on every field so a caller can patch one without resending the
-- rest — the editor saves the whole card, but the CLI fills one line at a time.
UPDATE growth_card SET
    title = COALESCE(sqlc.narg('title'), title),
    systems = COALESCE(sqlc.narg('systems'), systems),
    unknowns = COALESCE(sqlc.narg('unknowns'), unknowns),
    agent_plan = COALESCE(sqlc.narg('agent_plan'), agent_plan),
    understood = COALESCE(sqlc.narg('understood'), understood),
    verified = COALESCE(sqlc.narg('verified'), verified),
    learned = COALESCE(sqlc.narg('learned'), learned),
    next_gaps = COALESCE(sqlc.narg('next_gaps'), next_gaps),
    issue_id = CASE WHEN sqlc.arg('clear_issue')::boolean THEN NULL
                    ELSE COALESCE(sqlc.narg('issue_id'), issue_id) END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteGrowthCard :exec
DELETE FROM growth_card WHERE id = $1 AND workspace_id = $2;
