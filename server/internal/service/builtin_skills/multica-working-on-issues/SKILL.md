---
name: multica-working-on-issues
description: "Use when acting on a Multica issue beyond what the brief covers: PR linking vs close intent, reading a linked PR's real state, metadata keys, status-change side effects, sub-issue todo vs backlog."
user-invocable: false
allowed-tools: Bash(multica *), Bash(git *), Bash(gh *)
---

# Working on Multica issues

Product contracts the runtime brief does not fully encode: PR linking vs close
intent, reading linked-PR state, metadata keys, status side effects, and
sub-issue enqueue behavior.

For building mention links, load `multica-mentioning` instead — not this skill.

Every contract below is traced to source in
`references/working-on-issues-source-map.md`.

## PR linking and close intent are two distinct contracts

The GitHub webhook runs two separate scans over an incoming PR. They are not the
same gate and they read different fields.

**Linking** scans the PR **title, body, OR branch** for a routable issue key
(`PREFIX-NUMBER`, e.g. `MUL-2759`). Each match writes an issue ↔ PR link row.
This is the link that `multica issue pull-requests` reads back — but see the
reference-only rule below: a key that appears **only** as a bare mention in the
body is linked yet hidden from that list.

```text
MUL-2759: add built-in issue working skill        # title prefix → links, shown
agent/matt/mul-2759-working-on-issues             # branch ref   → links, shown
```

**Close intent** is stricter and is a separate scan over **title or body only —
never the branch**. It fires only for a key placed immediately after a closing
keyword (`Closes` / `Fixes` / `Resolves`, optional `:` then whitespace). That
adjacency is what sets the link row's close-intent flag, the gate that
auto-advances the issue to `done` when the PR merges.

```text
Closes MUL-2759                                    # links AND records close intent
Fixes MUL-2759
Resolves MUL-2759
Fix login MUL-2759                                 # links only — keyword not adjacent
```


**Reference-only links (hidden from the PR list).** A key that appears **only**
as a bare mention in the body — no closing keyword, and not in the title or
branch — still writes a link row, but the row is flagged `reference_only` and
**excluded from `multica issue pull-requests`** (and the issue's right-side PR
list in the UI). This keeps passing mentions like `Related MUL-2759` or
`Follow up in MUL-2759` from surfacing an unrelated PR as if it were working on
that issue. To make a PR show up for an issue, put the key in the title, the
branch, or after a closing keyword in the body — not as a loose body reference.

```text
Closes MUL-2759 in the body                        # links and shown
Related to MUL-2759 in the body (no title/branch)  # links but reference_only → hidden
```

### Default for code-changing issue work

When an issue run changes code in a checked-out GitHub repo, the default handoff
is to open or update a PR before posting the final Multica issue comment, unless
the user explicitly asked for a local-only change or no PR. This is a default, not
an unconditional command: if no code changed, say no PR is needed; if PR creation
is blocked by auth, failing tests, or missing remote state, report that blocker
instead of pretending the run is complete.

Use a routable issue key in the PR title, body, or branch so the webhook can
link the PR back to the issue; for close intent, place it after a closing
keyword per the rules above.

In the final issue comment, include the PR URL when a PR exists. If there is
none because no code changed or the user asked for none, say so explicitly.

## Reading a linked PR's real state

When a step depends on PR state, query Multica's link table — do not infer it
from branch names, GitHub search, memory, or `pr_url` metadata (which can be
stale).

```bash
multica issue pull-requests <issue-id> --output json
```

Returns `{"pull_requests": [...]}`. Each element exposes:

- `number`, `html_url`, `title`
- `state` — the PR lifecycle as a **single enum**, one of `merged`, `closed`,
  `draft`, `open`. There is no separate `draft` or `merged` boolean in the
  response; the server folds them into `state` (merged wins, then closed, then
  draft, else open).
- `merged_at` — non-null once merged; a second confirmation of `state: merged`.
- `provider` — `github`, `forgejo`, `gitea`, or `gitlab`.
- `mergeable_state` — mirrors GitHub (`clean` / `dirty` surfaced; other values
  round-trip as unknown; retained for compatibility).
- GitHub API snapshot fields: `snapshot_available`, `mergeable`,
  `merge_state_status`, `checks_rollup`, `checks_total`, `checks_passed`,
  `checks_failed`, `checks_running`, `failed_check_names`,
  `snapshot_fetched_at`, and `snapshot_stale`. `snapshot_available == true`
  means the feature is enabled and the snapshot matches the PR's current head.
  Only then does `checks_rollup == null` mean "no checks"; false means the
  snapshot feature is disabled, has not fetched yet, or only has an old head.
- `checks_conclusion` — coarse CI compatibility status: `passed`, `failed`,
  `pending`, or `null`. GitHub derives it from the current API snapshot;
  Forgejo/Gitea/GitLab derive it from webhook commit statuses. Backed by the
  provider-appropriate check counts.

So "is it merged?" is `state == "merged"` (or `merged_at != null`); "is it still
a draft?" is `state == "draft"`; coarse CI status is `checks_conclusion`.

If the command returns no linked PRs after a PR was opened, the link scanner did
not observe a routable issue key in the PR title/body/branch — or the only match
was a bare body mention, which links as `reference_only` and is hidden from this
list (see the reference-only rule above).

## Metadata: durable custom state

Metadata is a free-form KV bag of durable issue state. Reading metadata is safe.
Writing a metadata key is a state mutation and should be tied to an explicit
task requirement to record that state for later readers or runs. Keys are
whatever your workflow needs — the platform curates no vocabulary; pick short
snake_case names and reuse them consistently within your workspace.

Never store secrets, tokens, or API keys in metadata.
Not metadata: logs or summaries; runtime bookkeeping such as timestamps,
attempt counts, or agent IDs; or other single-run details such as
files touched and investigation notes — those belong in the result comment.

```bash
multica issue metadata set <issue-id> --key <key> --value <value>
multica issue metadata delete <issue-id> --key <stale-key>
```

`--value` is JSON-parsed by default (bool/number are sniffed); pass `--type
string|number|bool` to force a type.

## Custom properties: typed workflow state

Workspaces may define custom issue properties (Severity, Environment, QA
Status, ...). Properties are the typed, user-visible sibling of metadata:
values are validated against the definition (select options, date format,
http(s) URL), visible in the issue sidebar, and addressed by name.

- Read what exists before writing: `multica property list` shows the catalog;
  `multica issue property list <issue-id>` shows values set on the issue.
- Set values by property name and option name — the CLI translates to ids:

```bash
multica issue property set <issue-id> --name Environment --value staging
multica issue property set <issue-id> --name Platforms --value "iOS,Android"
multica issue property unset <issue-id> --name Environment
```

- A validation error lists the legal options — fix the value and retry.
- Definitions may include an optional catalog icon for visual identification;
  it does not change the property's type or value validation.
- Agents cannot create or edit property definitions (owner/admin humans only).
  If a needed property does not exist, propose it in a comment instead.
- Property vs metadata: if the value is workflow state a human should see and
  filter by, and a definition exists, prefer the property. Metadata stays the
  free-form bag for durable custom issue state.

## Status changes have server side effects

A status change is not cosmetic — the server enqueues or skips agent work based
on it. These are the contracts, not advice:

- **`backlog`** parks an agent-assigned issue: the assignee is set but no task
  fires. Moving `backlog → todo` (or any non-done/non-cancelled status) enqueues
  the assigned agent then.
- **`in_progress` / `in_review` on assignment runs** are agent-managed CLI
  mutations, not `StartTask` / `CompleteTask` side effects. The assignment
  runtime brief asks ordinary agents for `todo`/`backlog` → `in_progress` then
  `in_review` when they have delivered. Squad leaders share the opening
  `in_progress` step on the first assignment turn, keep the parent there while
  members work, and only move to `in_review` when a later re-trigger confirms
  the overall goal is met.
- **`in_review`** is an accepted issue status. Some workflows use it while a PR
  is open and awaiting review; moving to it is an explicit mutation.
- **`done`** on a child issue posts a system comment on its parent. If a PR
  carries close intent (`Closes MUL-XXXX`), it advances the issue to `done`
  itself on merge — you do not also need to flip it manually.
- **`cancelled`** is a terminal, user-driven decision to close the issue. Like
  `done` it enqueues no new agent work, but it does **not** stop tasks already in
  flight — a run in progress keeps going (MUL-4465). To stop a running task,
  cancel the task itself.
- **Failed issue-triggered tasks** may roll an issue from `in_progress` back to
  `todo` when no active task / retry remains — that is the main server-owned
  status write on the agent-run path.

## Commenting on one passage of the description

An ordinary comment is about the issue. An INLINE comment is about a specific
passage of its description — use it when the thing you are saying only makes
sense next to particular words: explaining a paragraph, questioning one
sentence, flagging a term.

```bash
multica issue comment add <id> --anchor "V1 结论" --content "..."
multica issue comment add <id> --anchor "V1 结论" --anchor-occurrence 2 --content "..."
```

`--anchor` takes the passage VERBATIM out of the current description. The CLI
locates it and computes the offset for you; do not try to supply one. Two rules
follow from that:

- **Copy, do not paraphrase.** A passage that does not appear exactly is a hard
  error, not a warning. That is deliberate: without it you would file a comment
  that silently highlights nothing, and the mistake would only surface later as
  "the feature is broken".
- **Disambiguate repeats with `--anchor-occurrence`.** When the passage occurs
  more than once, the error tells you how many times it occurs; pick the one
  you meant rather than widening the passage until it is unique.

The comment then behaves like any other: @mentions trigger, replies thread
under it, it can be resolved. The anchor only adds *where in the description*
it is about.

If the description is later edited so the passage no longer appears, the
comment survives and simply stops highlighting. Nothing is lost, so prefer an
anchored comment whenever the passage is what you are talking about.

## Reading one comment you were handed

```bash
multica issue comment get <comment-id>
```

Do NOT reach it by listing and filtering: the list endpoint returns the whole
thread, this returns one comment, and on a long discussion that difference is
most of your context budget. Takes the FULL UUID — the 8-character form in the
list table and on the app's comment card is a handle for humans, never a value
to retype. The response carries `issue_id` and `parent_id`, so one comment
reaches its issue and thread root without a search.

## Phases — filing what happened under the station it happened in

A long issue's comments arrive in one flat run — comment 3 and comment 30 can
belong to different stretches of work and nothing says so. A PHASE is a
container inside one issue holding the comments written while it was there.
Not a status: `status` forgets the route, a phase stays. **Every new issue is
created with 开始 → 评审 → 冻结 already on it** — file into those rather than
building a route first. Review recurs as `评审 2`, `评审 3`, each its own.

```bash
multica issue phase list <id>                      # NAME, STATE, COMMENTS, ENTERED, COMPLETED
multica issue phase add <id> 评审
multica issue phase enter <id> 评审                # record arrival
multica issue phase complete <id> 评审             # record departure
multica issue comment add <id> --phase 评审 --content "..."
multica issue comment list <id> --phase 评审
```

Rules that will bite you if you assume otherwise:

- **`<phase>` is the NAME** — case-insensitive, unique prefix accepted, full
  UUID also works. Exact-first, so `评审` still resolves once `评审 2` exists.
- **Names are unique per issue**; a duplicate returns **409** rather than two
  stations that read the same.
- **`complete` requires `enter` first** (**409** otherwise) — completing an
  unentered phase records a route the work never took. `enter` on an entered
  phase keeps the FIRST arrival and clears completion: coming back is not
  starting over.
- **State is derived**, never stored: completed → `done`, entered → `current`,
  neither → `pending`.
- **Deleting a phase deletes its comments**, so the CLI needs `--force` once it
  holds any. `phase list` shows that count — it is the only warning you get.
- **`comment list --phase` filters client-side**, so it is rejected with
  `--recent` / `--tail` / `--thread` / `--before`: those pick a window first,
  and the result would read as "everything in this phase" while being a slice.

Activities (status changes, description edits) carry no phase field. The UI
places them by TIME into whichever phase was current; the CLI does not group
them at all.

Issues predating this feature have no route — **not a bug to fix**. Adding
stations an issue never used fabricates a history rather than recovering one;
add one only when someone is about to file into it.

## A finished issue's title and description are frozen

Once an issue is `done` or `cancelled`, `PUT /api/issues/{id}` refuses any
request carrying `title` or `description` and returns **409**. That covers
`multica issue update --title/--description` too — the CLI goes through the
same endpoint.

A finished issue records what was true when it finished. The description is a
single current value, not a history, so a later edit leaves no way to tell
which version anyone acted on.

Everything else still works on a finished issue, and deliberately so: comments,
status, archiving, labels, metadata, reactions and relationships. A late
sibling finishing still posts its system comment on a done parent.

The lock opens by leaving the terminal status — the check reads the issue's
CURRENT status, so `done → in_progress` carries no body field and passes, and
the body is writable from the next request on. One request that both reopens
and rewrites is still refused; do it in two.

Found a mistake in a finished issue? Ask the user first, then either reopen →
correct → close again, or file the correction as a new issue. Do not reopen on
your own initiative.

### Reading one: it is a record, not the current state

The freeze is a rule about writing. Reading has its own, and it is the one
that costs you if you get it wrong: a finished issue describes **what was true
when it finished** — accurate about the past, silent about the present. A
design it describes may have been replaced last month and it neither knows
that nor says so. When `status` is `done` or `cancelled`:

- **Do not treat the description as current state.** Verify against the code,
  the config, or a live check first. `multica issue get` prints this on stderr.
- **Do not discount it either.** Why a decision was made and what was rejected
  is usually written down nowhere else and is still true. "Finished" is not
  "wrong"; skimming past a closed issue re-litigates settled decisions.
- **No automatic pointer to whatever replaced it.** Relations are only
  `blocks` / `blocked_by` / `related` plus parent/child — none means
  "superseded by". A recorded successor is a metadata key, so check
  `multica issue metadata list <id>` for `superseded_by`, and when you finish
  an issue that replaces an older one, record it there:

  ```bash
  multica issue metadata set <old-id> --key superseded_by --value COC-99
  ```

## Archiving is not a status

`archived` is a separate dimension from `status`, and the two answer different
questions:

- **`status`** — how the work ended: `done`, `cancelled`, `blocked`.
- **archived** — whether the card should still be in view.

Do NOT reach for `cancelled` to get a finished issue off the board. Cancelling
records that the work was abandoned, which is wrong for something that shipped,
and the distinction is unrecoverable afterwards. Archive it instead: the issue
keeps whatever status it ended on.

```bash
multica issue archive <id>      # takes the issue AND its sub-issue subtree out of view
multica issue unarchive <id>    # brings the same subtree back
multica issue list --include-archived
```

Two consequences worth knowing before you use it:

- **The whole subtree moves.** Archiving a requirement archives every sub-issue
  under it, at any depth. Archive a mid-tree node and only that node's own
  subtree goes; ancestors are untouched.
- **Archived issues stay readable.** `issue get <id>` still returns an archived
  issue; only list/board surfaces hide it. Links from other issues keep working.

Re-archiving an already-archived issue is a `409`, not a silent no-op — the
original `archived_at` is preserved so "when did this leave the board" stays
answerable.

### Deleting is not archiving

```bash
multica issue delete <id>           # permanent; refuses if the issue has sub-issues
multica issue delete <id> --force   # deletes it anyway, ORPHANING its sub-issues
```

Delete destroys the issue with its comments, reactions and attachments, and it
cannot be undone. Reach for `archive` unless the issue should genuinely stop
existing.

The one consequence worth knowing before you use `--force`: sub-issues are
**not** deleted with their parent. The parent link is `ON DELETE SET NULL`, so
they survive as top-level issues with no parent. `delete` refuses by default
when children exist precisely so that becomes a decision rather than something
you discover afterwards. If you want a whole tree gone from view, archive it —
archiving takes the subtree; deleting does not.

## `issue create` assigns to the caller unless told otherwise

`multica issue create` with no `--assignee` / `--assignee-id` assigns the new
issue to the member behind the current token. Pass `--no-assignee` to leave it
unassigned.

This does not change anything for you: an agent token has no member identity,
so the default resolves to nothing and the issue is created unassigned exactly
as before. Keep passing `--assignee <agent>` when handing work to an agent —
that is still the only way the issue reaches one.

## Mark a throwaway issue as a test

An issue you create to try something out must be marked, or it sits in the
list looking like real work.

```bash
multica issue create --test --title "封存横幅是否插到正文顶部"
# [测试] 封存横幅是否插到正文顶部
```

`--test` prefixes `[测试]`, idempotently. Use it for anything you would delete
afterwards, and delete them when you are done. Not going through the CLI,
write the prefix yourself — the convention is the title, not the flag, and it
is a prefix rather than a label because `issue list` has no labels column.

## Sub-issues: `todo` starts work now, `backlog` parks it

On an agent-assigned issue, create status decides whether the assignee fires
immediately. A non-backlog status (e.g. `todo`) enqueues the agent at create
time; `backlog` sets the assignee without triggering.

Parallel children — all start now:

```bash
multica issue create --title "..." --parent <issue-id> --assignee <agent> --status todo
```

Strictly serial children — park later steps, promote one at a time:

```bash
multica issue create --title "Step 2: ..." --parent <issue-id> --assignee <agent> --status backlog
multica issue status <child-id> todo   # promote when the previous step is truly done
```

Creating every serial step as `todo` enqueues the whole chain at once.

### Stages: order sub-issues into barrier groups

`--stage <N>` (N ≥ 1) groups sub-issues under the same parent into ordered
stages. The parent assignee is woken **once, when a whole stage finishes** —
i.e. every sub-issue in the lowest unfinished stage has reached a terminal
status (`done`/`cancelled`). A completion that does not close a stage is silent
(no comment, no wake). A sibling set with **no** stages is one implicit stage,
so the parent is woken once when the *last* sub-issue finishes — not on every
child.

Advancement is agent-driven: the server only detects the closed barrier and
wakes the parent assignee, who then decides whether to promote the next stage's
`backlog` sub-issues to `todo`.

```bash
# Stage 1 runs now; later stages parked until promoted
multica issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Research B" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Build"      --parent <id> --assignee <agent> --stage 2 --status backlog
multica issue create --title "Ship"       --parent <id> --assignee <agent> --stage 3 --status backlog
```

When both Stage 1 sub-issues finish you (the parent assignee) are woken with a
"Stage 1 complete" comment. Inspect the layout, then promote the next stage:

```bash
multica issue children <parent-id>             # sub-issues grouped by stage
multica issue status <stage-2-child-id> todo   # promote when its deps are met
```

Read each sub-issue's description before promoting and only promote items whose
stated dependencies are met; if a description conflicts with the parent's
breakdown, leave it `backlog` and comment to confirm first.

## References

`references/working-on-issues-source-map.md` — accurate `file:line` for every
contract above: the `pull-requests` CLI and route, the PR response field list,
`derivePRState`, the two-path link (`extractIdentifiers`) vs close-intent
(`extractClosingIdentifiers`) proof, the backlog enqueue lines, child-done
notify, the stage column / `stageBarrierClosed` barrier and the `--stage` /
`issue children` CLI, and the metadata CLI. Re-derive before depending on an
exact line.
