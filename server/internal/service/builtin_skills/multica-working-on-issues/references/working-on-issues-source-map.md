# working-on-issues source map

Evidence layer for `SKILL.md`. Every contract the skill states is traced to a
current `file:line` here. Lines were re-derived against `feat/builtin-skills`
after the latest `main` merge; the prior skill cited pre-merge lines that have
since moved (see the "drifted" column). Re-confirm with the verification command
at the bottom before relying on an exact line.

## `multica issue comment add --anchor` — comment on one passage

| Behavior | File:line |
|---|---|
| `--anchor` flag | `server/cmd/multica/cmd_issue.go:597` |
| `--anchor-occurrence` flag | `server/cmd/multica/cmd_issue.go:602` |
| Fetches the description and resolves the offset | `server/cmd/multica/cmd_issue.go:2088` (`locateAnchorInDescription`) |
| Character-based search + occurrence selection | `server/cmd/multica/cmd_issue.go:2121` (`anchorOffsetInText`) |
| Server-side validation of the anchor | `server/internal/handler/comment.go:1473` (`parseCommentAnchor`) |
| Columns | `server/migrations/270_comment_anchor.up.sql` |

The offset is in CHARACTERS, not bytes — the editor re-locates anchors in the
same coordinate system, and a byte offset lands mid-character on any CJK
description. The CLI resolves it from the live description rather than trusting
a caller-supplied number, which is also what turns a mistyped passage into an
immediate error instead of a comment that never highlights.

## `multica issue delete` — permanent, and does not take the subtree

| Behavior | File:line |
|---|---|
| CLI command `delete <id>` | `server/cmd/multica/cmd_issue.go:229` |
| `--force` (delete despite sub-issues) | `server/cmd/multica/cmd_issue.go:481` |
| `runIssueDelete`, incl. the sub-issue guard | `server/cmd/multica/cmd_issue.go:1486` |
| Route `DELETE /api/issues/{id}` | `server/cmd/server/router.go:1145` |
| Children survive as orphans | `issue_parent_issue_id_fkey` is `ON DELETE SET NULL` (`confdeltype = 'n'`) |

The guard is client-side: the API deletes unconditionally. It exists because
the orphaning is invisible at the call site — the request succeeds, the parent
is gone, and the children are silently promoted to top level.

## `multica issue archive` / `unarchive` — visibility, not status

| Behavior | File:line |
|---|---|
| CLI command `archive <id>` | `server/cmd/multica/cmd_issue.go:229` |
| CLI command `unarchive <id>` | `server/cmd/multica/cmd_issue.go:239` |
| `--include-archived` on `issue list` | `server/cmd/multica/cmd_issue.go:462` |
| `runIssueArchiveState` drives both directions | `server/cmd/multica/cmd_issue.go:1461` |
| Routes `POST /api/issues/{id}/archive` and `/unarchive` | `server/cmd/server/router.go:1143` |
| Handler `setIssueArchived` | `server/internal/handler/issue.go:3300` |
| Re-archiving returns 409, preserving the original `archived_at` | `server/internal/handler/issue.go:3307` |
| Subtree walk (archives descendants at any depth) | `server/pkg/db/queries/issue.sql:393` (`ArchiveIssueSubtree`) |
| `archived_at` / `archived_by` columns | `server/migrations/251_issue_archive.up.sql` |
| List default hides archived | `server/internal/handler/issue.go:1035` |
| Board / table default hides archived | `server/internal/handler/issue_table_query.go:652` |

Archiving writes only `archived_at` / `archived_by`; `status` is never touched,
which is what keeps "how did this end" answerable after the card leaves the
board. `GET /api/issues/{id}` has no archive filter, so an archived issue stays
readable by id and by link.

## `multica issue pull-requests` — read PR links from Multica

| Behavior | File:line | Drifted from |
|---|---|---|
| CLI command `pull-requests <id>` (alias `prs`) | `server/cmd/multica/cmd_issue.go:105` | `:104` |
| `runIssuePullRequests` handler | `server/cmd/multica/cmd_issue.go:507` | new citation |
| Calls `GET /api/issues/<id>/pull-requests` | `server/cmd/multica/cmd_issue.go:522` | `:522` (unchanged) |
| API route registration | `server/cmd/server/router.go:480` | `:480` (unchanged) |
| Handler `ListPullRequestsForIssue` → `Queries.ListPullRequestsByIssue` | `server/internal/handler/github.go:687,692` | `:466` |
| Row → response mapper `issuePullRequestRowToResponse` | `server/internal/handler/github.go:205` | `:149` |

The CLI resolves the issue ref, GETs the endpoint, and (for `--output json`)
prints the raw `{"pull_requests": [...]}` body. Only `--output` is accepted; the
default `table` shows `NUMBER STATE TITLE URL`.

## PR response shape

`GitHubPullRequestResponse` struct: `server/internal/handler/github.go:58`. JSON
fields the agent can read off each element of `pull_requests`:

- `provider` (`json:"provider"`, line 63)
- `number` (`json:"number"`, line 67)
- `html_url` (`json:"html_url"`, line 70)
- `title` (`json:"title"`, line 68)
- `state` (`json:"state"`, line 69) — the folded lifecycle enum (see below)
- `merged_at` (`json:"merged_at"`, line 74), `closed_at` (line 75)
- `mergeable_state` (`json:"mergeable_state"`, line 80) — mirrors GitHub; UI only
  surfaces `clean`/`dirty`, other values round-trip as unknown
- `snapshot_available` (`json:"snapshot_available"`, line 100) — for GitHub,
  true only when the App snapshot feature is enabled and the snapshot head
  matches the current PR head (`currentGitHubSnapshotAvailable`, lines 258-265)
- `mergeable` / `merge_state_status` (lines 90, 94) — conflict-only verdict vs
  the complete merge gate; "ready" requires `merge_state_status == "clean"`
- `checks_rollup` (`json:"checks_rollup"`, line 105) and run-level
  `checks_total` / `checks_passed` / `checks_failed` / `checks_running`
  (lines 111-114), plus `failed_check_names` (line 118)
- `checks_conclusion` (`json:"checks_conclusion"`, line 108) — coarse
  `"passed"`/`"failed"`/`"pending"` or `null`; GitHub derives it only from an
  available current-head snapshot (mapper lines 242-254), while self-hosted VCS
  providers use `aggregateChecksConclusion` (line 275)

There is **no** standalone `draft` or `merged` boolean in the response. The
PR lifecycle is encoded in the single `state` string by `derivePRState`
(`server/internal/handler/github.go:1317`):

```
merged   → if PullRequest.Merged
closed   → else if PullRequest.State == "closed"
draft    → else if PullRequest.Draft
open     → otherwise
```

`derivePRState` is called when the webhook upserts the row
(`server/internal/handler/github.go:1115`), so `state` is what the list endpoint
returns. "Is it merged?" = `state == "merged"` (or `merged_at != null`); "is it a
draft?" = `state == "draft"`. Combine with `checks_conclusion` for CI status.

## Two distinct webhook paths: link vs close-intent

Both run inside the `pull_request` webhook handler, gated by the workspace
auto-link flag (`workspaceAutoLinkPRsEnabled`, `github.go:1074`).

### Path 1 — link (title OR body OR branch)

- `extractIdentifiers` regex helper: `server/internal/handler/github.go:1028`
- driving regex `identifierRe` (`\b([a-z][a-z0-9]{1,9})-(\d+)\b`, case-insensitive):
  `server/internal/handler/github.go:490`
- call site: `server/internal/handler/github.go:727` —
  `extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)`

Every `PREFIX-NUMBER` mention in **title, body, or branch** resolves to an issue
in the workspace and writes a link row (`LinkIssueToPullRequest`, ~`github.go:762`).
This is what `multica issue pull-requests` later reads back.

**Reference-only flag (MUL-3739).** The link row carries a `reference_only`
boolean (`migrations/127_issue_pull_request_reference_only.up.sql`). The handler
computes a `qualifyingIdents` set = identifiers in **title or branch** (any
`extractIdentifiers` match) ∪ **body closing keywords** (`closingIdents`). A
linked identifier NOT in that set was matched only by a bare body mention, so its
row is written with `reference_only = true`. Both `ListPullRequestsByIssue` and
`GetIssuePullRequestCloseAggregate` filter `AND NOT reference_only`, so
reference-only links are hidden from the CLI / UI PR list **and** excluded from
the auto-advance gate (an open body-only mention must not silently block the
issue from reaching `done` while invisible in the list). The row still exists for
edit-time close-intent tracking. `reference_only` follows the same
`preserve_close_intent` terminal gate as `close_intent`.

Drifted from the prior skill's `github.go:727` citation, which pointed at the old
call-site location for the link logic.

### Path 2 — close intent (title OR body only, keyword-adjacent)

- `extractClosingIdentifiers` regex helper: `server/internal/handler/github.go:1051`
- driving regex `closingIdentifierRe`
  (`\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[:\s]+([a-z][a-z0-9]{1,9})-(\d+)\b`):
  `server/internal/handler/github.go:501`
- call site: `server/internal/handler/github.go:736` —
  `extractClosingIdentifiers(p.PullRequest.Title, p.PullRequest.Body)` (no branch arg)

Only a `PREFIX-NUMBER` immediately after a closing keyword
(`Closes`/`Fixes`/`Resolves`, optional `:` then whitespace) sets the link row's
`close_intent` flag — the gate that auto-advances the issue to `done` on merge.
`Fix MUL-1` closes; `Fix login MUL-1` does not (adjacency). Branch names are
deliberately excluded (function doc, `github.go:1044-1050`): a branch like
`mul-1/fix-login` links but must never declare close intent.

Drifted from the prior skill's `github.go:736` citation.

Net: a bare title prefix (`MUL-2759: ...`) or a branch ref links only (shown in
the PR list); `Closes MUL-2759` links **and** records close intent; a bare body
mention with no title/branch ref and no closing keyword links as `reference_only`
and is hidden from the PR list.

## Status side effects (enqueue contracts)

| Behavior | File:line | Drifted from |
|---|---|---|
| Create-time: agent-assigned, non-backlog issue enqueues immediately | `server/internal/handler/issue.go:2263-2264` | new citation |
| `shouldEnqueueAgentTask` returns false for `backlog` (parking lot) | `server/internal/handler/issue.go:2644-2648` | new citation |
| Backlog → non-backlog (not done/cancelled) enqueues on update | `server/internal/handler/issue.go:2537-2540` | `:2523` |
| Same contract in batch update | `server/internal/handler/issue.go:3021-3024` | new citation |
| Child → `done` notifies + wakes the parent, gated by the stage barrier | `server/internal/handler/issue_child_done.go:66` (`notifyParentOfChildDone`; doc comment at `:15`; barrier gate at `:115`) | func def `:51` |
| Status change (incl. → `cancelled`) does NOT cancel in-flight tasks; only issue deletion does (MUL-4465) | no-cancel note in `server/internal/handler/issue.go:2652-2658` (`UpdateIssue`) and `:3170-3171` (`BatchUpdateIssues`); deletion still cancels at `:2863` (`DeleteIssue`) / `:3239` (`BatchDeleteIssues`) via `CancelTasksForIssue` (`server/internal/service/task.go:1229`) | new citation |
| `StartTask` / `CompleteTask` do not write issue status (agent CLI owns progress) | `server/internal/service/task.go` (`StartTask` / `CompleteTask` comments) | new citation |
| Assignment brief: ordinary agent `in_progress` then `in_review`; squad leader `in_progress` only on first dispatch | `server/internal/daemon/execenv/runtime_config_sections.go` (`writeWorkflowAssignment`) | new citation |
| Failed task may roll `in_progress` → `todo` when no active task remains | `server/internal/service/task.go` (`HandleFailedTasks`) | new citation |

Creation with `--status todo` (or any non-backlog status) on an agent-assigned
issue fires the agent immediately; `--status backlog` parks it with the assignee
set but no trigger. Promoting `backlog → todo` later fires it then (update path,
line 2537).

Moving an issue to `cancelled` used to call `CancelTasksForIssue` and stop every
active task on it (the old #940 behavior). MUL-4465 removed that from both
`UpdateIssue` and `BatchUpdateIssues`: a status flip — `cancelled` included —
never cancels tasks now. `CancelTasksForIssue` fires only from the issue-deletion
paths (`DeleteIssue` / `BatchDeleteIssues`), where the owning issue row is going
away, so no task is left orphaned.

## Sub-issue stages (barrier wake)

| Behavior | File:line |
|---|---|
| `issue.stage` column (nullable, `>= 1`) | `server/migrations/123_issue_stage.up.sql` |
| Stage barrier: notify+wake fire only when the lowest unfinished stage is all-terminal; unstaged set = one implicit stage | `server/internal/handler/issue_child_done.go:231` (`stageBarrierClosed`) |
| Per-stage summary + next stage for the wake comment | `server/internal/handler/issue_child_done.go:254` (`stageProgressSummary`) |
| Terminal issues (`done` / `cancelled`) reject `title` / `description` writes with 409; judged on the issue's current status so leaving the status unlocks it | `server/internal/handler/issue.go` (`allowIssueBodyWrite`, `isTerminalIssueStatus`) |
| `--stage` on `issue create` / `issue update` | `server/cmd/multica/cmd_issue.go:328,350` |
| `issue create` defaults the assignee to the caller's member id (`/api/me`); `--no-assignee` opts out. Agent tokens have no member identity, so the lookup returns nothing and the issue stays unassigned | `server/cmd/multica/cmd_issue.go` (`currentMemberID`) |
| `multica issue children <id>` (sub-issues grouped by stage) | `server/cmd/multica/cmd_issue.go:114,678`; route `GET /api/issues/{id}/children` → `ListChildIssues` |

Advancement is agent-driven: the server only detects the closed barrier and
wakes the parent assignee. Promoting the next stage's `backlog` sub-issues to
`todo` is the woken agent's decision, not a server side effect. When the woken
assignee (often a squad leader) decides the parent is complete, the system
comment explicitly asks for `multica issue status <parent-id> in_review` —
comment-triggered runs otherwise must not change status unless asked.

## Metadata CLI

| Behavior | File:line |
|---|---|
| `multica issue metadata set <issue-id> --key --value [--type]` | `server/cmd/multica/cmd_issue_metadata.go:80,109-111` |
| `multica issue metadata delete <issue-id> --key` | `server/cmd/multica/cmd_issue_metadata.go:93,113` |
| API routes (PUT/DELETE `/metadata/{key}`) | `server/cmd/server/router.go:478-479` |

`--value` is JSON-parsed by default (bool/number sniff); `--type` forces
`string`/`number`/`bool`.

## Custom properties CLI

| Behavior | File:line |
|---|---|
| `multica property list/get/create/update/archive/unarchive` | `server/cmd/multica/cmd_property.go` |
| `multica issue property list/set/unset` (name→id translation) | `server/cmd/multica/cmd_property.go` (`encodeIssuePropertyValue`) |
| Definition CRUD, admin gate, agent-actor rejection | `server/internal/handler/property.go` (`requirePropertyAdmin`) |
| Optional catalog icon field and allowlist validation | `server/internal/handler/property.go` (`PropertyResponse`, `validatePropertyIcon`) |
| Per-type value validation (self-correcting errors) | `server/internal/handler/property.go` (`validatePropertyValue`) |
| API routes (`/api/properties`, PUT/DELETE `/api/issues/{id}/properties/{propertyId}`) | `server/cmd/server/router.go` |

## A finished issue: frozen body, and a record on read

| Behavior | File:line |
|---|---|
| Terminal statuses (`done`, `cancelled`) | `server/internal/handler/issue.go:2828` (`isTerminalIssueStatus`) |
| Fields the freeze covers | `server/internal/handler/issue.go:2822` (`issueBodyFields` — title, description) |
| 409 on a body write, judged on the CURRENT status | `server/internal/handler/issue.go:2849` (`allowIssueBodyWrite`) |
| Enforced at the update entry point | `server/internal/handler/issue.go:2894` |
| Handler tests (7 cases) | `server/internal/handler/issue_freeze_test.go` |
| `issue get` prints the record notice on stderr | `server/cmd/multica/cmd_issue.go:894,948` (`warnTerminalIssueIsARecord`) |
| CLI mirror of the terminal status set | `server/cmd/multica/cmd_issue.go:929` (`terminalIssueStatuses`) |
| Notice tests, incl. the "do not call it expired" guard | `server/cmd/multica/cmd_issue_terminal_notice_test.go` |
| Read-only body + both hints in the app | `packages/views/issues/components/issue-detail.tsx:1990,2923` |

The freeze is a WRITE rule; the notice is the READ rule, and they say different
things. The write rule is "you cannot change this". The read rule is "this
describes what was true when the work finished" — accurate about the past,
silent about the present. The notice deliberately avoids the words expired /
outdated / obsolete / stale (a test enforces this): why a decision was made and
what was rejected are usually recorded nowhere else and are still true, and an
agent told the issue is stale skips exactly that.

Nothing links a finished issue to whatever replaced it — relations are only
`blocks` / `blocked_by` / `related` (`server/migrations/001_init.up.sql:93`)
plus parent/child. A successor has to be recorded by hand as the
`superseded_by` metadata key, which is where the notice points.

## `multica issue comment get` — read one comment by id

| Behavior | File:line |
|---|---|
| CLI command `comment get <comment-id>` | `server/cmd/multica/cmd_issue.go:295` |
| Full-UUID check before any request | `server/cmd/multica/cmd_issue.go:2311` (`runIssueCommentGet`) |
| API route `GET /api/comments/{commentId}` | `server/cmd/server/router.go:1328` |
| Handler, workspace-scoped load | `server/internal/handler/comment.go:3417` (`GetComment`) |
| Copyable id chip on the comment card | `packages/views/issues/components/comment-card.tsx` (`CommentIdChip`) |

The chip displays 8 characters and copies 36: the CLI takes nothing shorter,
so the short form is a handle, not a value. An id from another workspace reads
as 404 rather than 403 — a permission error would confirm the comment exists.

## Phases CLI

| Behavior | File:line |
|---|---|
| `multica issue phase list/add/enter/complete/rename/delete` | `server/cmd/multica/cmd_issue_phase.go:22,42,54,65,75,83` |
| Name-first reference resolution (exact, then unique prefix, then UUID) | `server/cmd/multica/cmd_issue_phase.go:175` (`resolveIssuePhase`) |
| State derived from the two timestamps, never stored | `server/cmd/multica/cmd_issue_phase.go:130` (`phaseState`) |
| `--force` required before deleting a phase that holds comments | `server/cmd/multica/cmd_issue_phase.go:434` |
| `issue comment add --phase` flag → resolved to `phase_id` | `server/cmd/multica/cmd_issue.go:606,2278` |
| `issue comment list --phase` flag, rejected with the windowing flags | `server/cmd/multica/cmd_issue.go:573,2022` |
| API routes (`/api/issues/{id}/phases`, `/enter`, `/complete`) | `server/cmd/server/router.go:1166-1174` |
| Duplicate name rejected with 409 | `server/internal/handler/issue_phase.go:119` |
| Complete without enter rejected with 409 | `server/internal/handler/issue_phase.go:216` |
| Deleting a phase deletes its comments | `server/internal/handler/issue_phase.go:292` (`DeleteCommentsInPhase`) |
| `phase_id` on comment create validated against the issue | `server/internal/handler/comment.go:1952` |
| Unique index backing the 409 | `server/migrations/268_issue_phase_unique_name.up.sql:8` |
| Enter keeps the first arrival and clears completion | `server/pkg/db/queries/issue_phase.sql:25` (`EnterIssuePhase`) |

The CLI resolves a phase NAME to its UUID before every call — the API only
takes UUIDs, and the name is what a person or an agent actually holds. Matching
is exact-before-prefix so adding `评审 2` cannot make the existing `评审`
ambiguous.

## Verification command

Re-derive any line above before depending on it:

```bash
cd server
grep -n 'pull-requests <id>'                 cmd/multica/cmd_issue.go
grep -n 'ListPullRequestsForIssue'           cmd/server/router.go internal/handler/github.go
grep -n 'func issuePullRequestRowToResponse\|type GitHubPullRequestResponse struct\|func derivePRState\|func extractIdentifiers\|func extractClosingIdentifiers\|closingIdentifierRe' internal/handler/github.go
grep -n 'extractIdentifiers(\|extractClosingIdentifiers(\|derivePRState(' internal/handler/github.go
grep -n 'qualifyingIdents\|reference_only\|ReferenceOnly' internal/handler/github.go pkg/db/queries/github.sql
grep -n 'prevIssue.Status == "backlog"\|func (h \*Handler) shouldEnqueueAgentTask' internal/handler/issue.go
grep -n 'func notifyParentOfChildDone'       internal/handler/issue_child_done.go
```
