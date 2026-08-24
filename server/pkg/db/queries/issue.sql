-- name: ListIssues :many
-- involves_user_id widens the assignee filter to surface issues where the user
-- is *indirectly* the assignee — via an owned agent or a squad they belong to /
-- lead / have an agent inside. The semantics intentionally exclude direct
-- member assignment (`assignee_type='member' AND assignee_id=involves_user_id`)
-- because that is already the meaning of the `assignee_id` filter (tab 1
-- "Assigned to me"), and the two filters must produce disjoint result sets.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
       i.description_revision
FROM issue i
WHERE i.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    -- (1) assignee is an agent owned by the user
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    -- (2)(3)(4) assignee is a squad related to the user — three relations
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          -- (2) the user is a human member of the squad
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          -- (3) the squad's canonical leader is an agent owned by the user.
          -- We read squad.leader_id directly rather than relying on a
          -- squad_member row, because the leader copy in squad_member is
          -- best-effort (see squad.go AddSquadMember error handling).
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          -- (4) the squad has an agent member owned by the user
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
  )
ORDER BY i.position ASC, i.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetIssue :one
SELECT * FROM issue
WHERE id = $1;

-- name: GetIssueGCStatus :one
SELECT workspace_id, status, updated_at
FROM issue
WHERE id = $1;

-- name: ListIssueGCStatuses :many
SELECT id, status, updated_at
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = ANY(sqlc.arg('issue_ids')::uuid[]);

-- name: GetIssueInWorkspace :one
SELECT * FROM issue
WHERE id = $1 AND workspace_id = $2;

-- name: LockIssueForChannelMediaBind :one
-- Channel media resolves after /issue creation. Hold a key-share lock while
-- the attachment row is written so a concurrent issue delete cannot land
-- between the workspace-scoped validation and the attachment insert.
SELECT id FROM issue
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockIssueForDescriptionUpdate :one
-- Serialize user description saves with detached channel-media appends. The
-- handler merges channel media that landed after the editor's submitted base
-- while holding this lock, then performs UpdateIssue in the same transaction.
-- The returned row carries description_revision, which the handler checks
-- against the caller's declared base BEFORE merging — a stale base has to be
-- refused, not merged into.
SELECT * FROM issue
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: MaterializeIssueChannelMediaMarkdown :one
-- Detached channel media resolves after /issue creation. When the description
-- still equals the exact creation-time base, replace its inline placeholders
-- with the fully composed Markdown so rich-text ordering survives. If a user
-- edited concurrently (or the adapter has no inline layout), append instead;
-- preserving user-authored bytes takes precedence over layout fidelity.
--
-- Deliberately does NOT move description_revision. This is the platform
-- attaching media the user already sent, not a competing author, and the
-- editor's save path merges it back non-destructively (see
-- mergeIssueChannelMediaDescription). Bumping here would 409 a save that the
-- merge is designed to accept, trading a solved problem for a new one.
UPDATE issue
SET description = CASE
        WHEN sqlc.narg('base_description')::text IS NOT NULL
             AND COALESCE(description, '') = sqlc.narg('base_description')::text
            THEN sqlc.arg('description')::text
        WHEN description IS NULL OR description = '' THEN sqlc.arg(markdown)
        ELSE description || E'\n\n' || sqlc.arg(markdown)
    END,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: LockIssueForDelete :one
-- Issue deletion must collect every attachment URL after it has won the same
-- row-lock race used by channel media binding. FOR UPDATE conflicts with the
-- binder's FOR KEY SHARE: either bind commits first and its URL is collected,
-- or delete commits first and the binder leaves its durable intent for cleanup.
SELECT id FROM issue
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateIssue :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    stage
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('stage')
) RETURNING *;

-- name: GetIssueByNumber :one
SELECT * FROM issue
WHERE workspace_id = $1 AND number = $2;

-- name: UpdateIssue :one
UPDATE issue SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = COALESCE(sqlc.narg('position'), position),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    parent_issue_id = sqlc.narg('parent_issue_id'),
    project_id = sqlc.narg('project_id'),
    stage = sqlc.narg('stage'),
    -- Only moves when the value actually changes. Comparing against the
    -- column in the same statement means a caller that passes the current
    -- status — which every full-object update does — leaves the timestamp
    -- alone, and no caller has to remember to say whether this counts as a
    -- transition.
    status_changed_at = CASE
        WHEN sqlc.narg('status')::text IS NOT NULL
         AND sqlc.narg('status')::text IS DISTINCT FROM status
        THEN now() ELSE status_changed_at
    END,
    -- The body's optimistic-concurrency counter, bumped in the same statement
    -- that writes the body so no window exists where one has moved and the
    -- other has not. Same shape as status_changed_at above: comparing against
    -- the column means a caller re-sending the text it already had leaves the
    -- counter alone, and a harmless no-op write cannot invalidate the base
    -- another writer is holding.
    description_revision = CASE
        WHEN sqlc.narg('description')::text IS NOT NULL
         AND sqlc.narg('description')::text IS DISTINCT FROM description
        THEN description_revision + 1 ELSE description_revision
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateIssueStatus :one
-- Workspace_id in the WHERE clause is a SQL-layer tenant guard; see DeleteIssue.
UPDATE issue SET
    status = $2,
    status_changed_at = CASE
        WHEN $2::text IS DISTINCT FROM status THEN now() ELSE status_changed_at
    END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: CreateIssueWithOrigin :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, stage
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'), sqlc.narg('stage')
) RETURNING *;

-- name: LockIssueDuplicateKey :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: FindActiveDuplicateIssue :one
SELECT * FROM issue
WHERE workspace_id = $1
  AND status NOT IN ('done', 'cancelled')
  AND project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND parent_issue_id IS NOT DISTINCT FROM sqlc.arg('parent_issue_id')::uuid
  AND lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')
ORDER BY created_at ASC
LIMIT 1;

-- name: FindRecentAutopilotDuplicateIssue :one
SELECT i.* FROM issue i
WHERE i.workspace_id = $1
  AND i.status NOT IN ('done', 'cancelled')
  AND i.origin_type = 'autopilot'
  AND i.origin_id = $2
  AND i.project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND lower(btrim(regexp_replace(i.title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')
  AND i.created_at >= sqlc.arg('created_after')::timestamptz
  AND EXISTS (
    SELECT 1
    FROM autopilot_run r
    WHERE r.issue_id = i.id
      AND r.autopilot_id = i.origin_id
      AND r.status IN ('issue_created', 'running', 'completed')
  )
ORDER BY i.created_at ASC
LIMIT 1;

-- name: DeleteIssue :exec
-- Defense-in-depth: the workspace_id predicate makes the tenant invariant a
-- SQL-layer guarantee rather than a handler-layer one. Handler loaders
-- (loadIssueForUser / GetIssueInWorkspace) already enforce membership today,
-- but a future loader bypass or a new caller skipping the loader would be
-- silently catastrophic without this guard. See incident #1661.
--
-- issue_vcs_pull_request (migration 213) has no FK to issue, so the link rows
-- are not cascaded away. Sweep them here so they go atomically with the issue.
-- The mirrored PR rows themselves belong to the connection, not the issue, so
-- they persist (matching the GitHub link behaviour).
--
-- The sweep MUST route through the same workspace-checked target as the issue
-- delete: deleting links by bare issue_id would drop another tenant's link rows
-- when a caller passes a foreign issue_id with its own workspace_id (the issue
-- itself is correctly untouched, but the links are already gone) — the exact
-- cross-tenant leak the #1661 guard above exists to prevent.
WITH target AS (
    SELECT issue.id FROM issue WHERE issue.id = $1 AND issue.workspace_id = $2
),
cleared_vcs_pr_links AS (
    DELETE FROM issue_vcs_pull_request WHERE issue_id IN (SELECT target.id FROM target)
)
DELETE FROM issue WHERE issue.id IN (SELECT target.id FROM target);

-- name: ListOpenIssues :many
-- See ListIssues for the semantics of involves_user_id (mirrors the 4-branch
-- filter; member-direct assignment is intentionally excluded).
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties
FROM issue i
WHERE i.workspace_id = $1
  AND i.status NOT IN ('done', 'cancelled')
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  -- properties_filter is a jsonb array of groups, each group an array of
  -- containment patterns (built by parsePropertiesFilterParam): the issue
  -- must match at least one pattern from EVERY group (AND of ORs). The
  -- correlated form skips the GIN index, which is fine here: open_only is
  -- an unpaginated workspace scan already narrowed by status.
  AND (
    sqlc.narg('properties_filter')::jsonb IS NULL
    OR NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements(sqlc.narg('properties_filter')::jsonb) AS pf(alternatives)
      WHERE NOT EXISTS (
        SELECT 1
        FROM jsonb_array_elements(pf.alternatives) AS alt(pattern)
        WHERE i.properties @> alt.pattern
      )
    )
  )
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
  )
ORDER BY i.position ASC, i.created_at DESC;

-- name: CountIssues :one
-- See ListIssues for the semantics of involves_user_id.
SELECT count(*) FROM issue i
WHERE i.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
  );

-- name: ListChildIssues :many
-- Order by position, falling back to number.
--
-- This used to be `number ASC` alone, on the reasoning that position is
-- computed per-(workspace, status) rather than relative to siblings and would
-- therefore interleave children unpredictably. That reasoning still holds for
-- position ALONE — but the issue table already sorts its hierarchy rows by
-- i.position (see issueTableOrderBy), so the same parent's children were
-- ordered one way in the tree and another way here. Two orders for one list is
-- worse than an imperfect one: a user who drags a sub-issue in the table then
-- opens the parent finds their change apparently ignored.
--
-- `number` as the tiebreaker is what makes this safe. Children that were never
-- reordered share no position ordering worth trusting, and they fall back to
-- the same stable creation order this query has always produced. Only children
-- someone has actually moved sort ahead of that.
SELECT * FROM issue
WHERE parent_issue_id = $1
ORDER BY position ASC, number ASC;

-- name: ListChildrenByParents :many
-- Batched variant of ListChildIssues: returns all children for the given
-- parent set in one round trip. Used by Swimlane to avoid an N+1 fan-out
-- (one request per visible parent lane). Result is grouped client-side by
-- parent_issue_id; the workspace filter is also enforced so callers can't
-- enumerate children of parents in workspaces they don't belong to.
-- Within each parent, order by (position, number) for the same sibling order
-- as ListChildIssues — see the rationale there.
SELECT * FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND parent_issue_id = ANY(sqlc.arg('parent_ids')::uuid[])
ORDER BY parent_issue_id, position ASC, number ASC;

-- name: GetIssueByOrigin :one
-- Finds the issue stamped with a specific (origin_type, origin_id) pair.
-- Used by quick-create completion to deterministically locate the issue
-- produced by a given agent_task_queue.id — robust against concurrent
-- issue creates by the same agent (assignment task + quick-create both
-- running with max_concurrent_tasks > 1).
SELECT * FROM issue
WHERE workspace_id = $1
  AND origin_type = $2
  AND origin_id = $3
LIMIT 1;

-- name: CountCreatedIssueAssignees :many
-- Count assignees on issues created by a specific user.
SELECT
  assignee_type,
  assignee_id,
  COUNT(*)::bigint as frequency
FROM issue
WHERE workspace_id = $1
  AND creator_id = $2
  AND creator_type = 'member'
  AND assignee_type IS NOT NULL
  AND assignee_id IS NOT NULL
GROUP BY assignee_type, assignee_id;

-- name: ChildIssueProgress :many
SELECT parent_issue_id,
       COUNT(*)::bigint AS total,
       COUNT(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done
FROM issue
WHERE workspace_id = $1
  AND parent_issue_id IS NOT NULL
GROUP BY parent_issue_id;

-- SearchIssues: moved to handler (dynamic SQL for multi-word search support).

-- name: SetIssueMetadataKey :one
-- Atomically sets a single key in the issue's metadata JSONB. The
-- workspace_id filter is the authorization gate — handler resolves the
-- issue first so this is also the tenant check.
UPDATE issue SET
    metadata = jsonb_set(metadata, ARRAY[sqlc.arg('key')::text], sqlc.arg('value')::jsonb),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssueMetadataKey :one
-- Atomically removes a single key from the issue's metadata JSONB.
-- Deleting a missing key is a no-op (still returns the row).
UPDATE issue SET
    metadata = metadata - sqlc.arg('key')::text,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: MarkIssueFirstExecuted :one
-- Flips first_executed_at from NULL to now() atomically. Returns the row if
-- this was the first time the issue was executed; no rows otherwise. The
-- analytics issue_executed event fires exactly when this returns a row —
-- retries and re-assignments hit the WHERE clause and no-op.
UPDATE issue
SET first_executed_at = now()
WHERE id = $1 AND first_executed_at IS NULL
RETURNING id, workspace_id, creator_type, creator_id, first_executed_at;

-- name: ArchiveIssueSubtree :many
-- Archives an issue together with everything below it. A requirement and its
-- sub-issues leave the board as one unit — archiving only the parent would
-- leave its children stranded in the list with no visible context.
--
-- Already-archived rows are skipped so re-running never rewrites an earlier
-- archived_at. `UNION` rather than `UNION ALL` so malformed cyclic data
-- terminates.
WITH RECURSIVE subtree AS (
    SELECT root.id
    FROM issue root
    WHERE root.id = sqlc.arg('id') AND root.workspace_id = sqlc.arg('workspace_id')
    UNION
    SELECT child.id
    FROM issue child
    JOIN subtree ON child.parent_issue_id = subtree.id
    WHERE child.workspace_id = sqlc.arg('workspace_id')
)
UPDATE issue
SET archived_at = now(), archived_by = sqlc.arg('archived_by'), updated_at = now()
WHERE id IN (SELECT id FROM subtree) AND archived_at IS NULL
RETURNING *;

-- name: UnarchiveIssueSubtree :many
-- Mirror of ArchiveIssueSubtree. Restores the issue and its descendants.
WITH RECURSIVE subtree AS (
    SELECT root.id
    FROM issue root
    WHERE root.id = sqlc.arg('id') AND root.workspace_id = sqlc.arg('workspace_id')
    UNION
    SELECT child.id
    FROM issue child
    JOIN subtree ON child.parent_issue_id = subtree.id
    WHERE child.workspace_id = sqlc.arg('workspace_id')
)
UPDATE issue
SET archived_at = NULL, archived_by = NULL, updated_at = now()
WHERE id IN (SELECT id FROM subtree) AND archived_at IS NOT NULL
RETURNING *;

-- name: ParkIssue :one
-- Lifts an issue out of its parent so the parent can finish without it.
--
-- Three writes that only make sense together: detach from the parent (so
-- subtree archiving no longer takes it), drop to backlog (it is explicitly
-- not current work), and record where it came from (an "optimize this later"
-- with no origin is unreadable three months on).
--
-- parked_from is passed in rather than read from parent_issue_id here so the
-- handler can keep an earlier origin when an already-parked issue is parked
-- again from somewhere else, and so parking a top-level issue records nothing.
UPDATE issue
SET parent_issue_id      = NULL,
    stage                = NULL,
    status               = 'backlog',
    parked_from_issue_id = sqlc.narg('parked_from_issue_id'),
    updated_at           = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ListParkedFromIssue :many
-- What was parked out of this requirement, oldest first. Excludes archived
-- rows: a parked issue that was later archived is done with.
SELECT * FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND parked_from_issue_id = sqlc.arg('parked_from_issue_id')
  AND archived_at IS NULL
ORDER BY created_at ASC;
