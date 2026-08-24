---
name: multica-working-on-issues
description: "Use when acting on a Multica issue beyond what the brief covers: PR linking vs close intent, reading a linked PR's real state, comment threads and when to resolve one, metadata keys, status-change side effects, sub-issue todo vs backlog."
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

Returns `{"pull_requests": [...]}`. The three answers you usually want:
"is it merged?" is `state == "merged"` (or `merged_at != null`); "is it still a
draft?" is `state == "draft"`; coarse CI status is `checks_conclusion`
(`passed` / `failed` / `pending` / `null`).

`state` is a SINGLE enum — `merged`, `closed`, `draft`, `open` — with no
separate draft or merged boolean beside it. Every field on the element, and the
snapshot rules behind `checks_rollup`, are in
`references/pull-request-response.md`.

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
- **Interactive `done` changes review open comment threads first.** A 409 with
  `code=comment_review_required` leaves the issue unchanged and returns every
  blocking root plus its `last_activity_at` snapshot. Read every thread and
  resolve only the discussions you judge must be closed before Done; for each
  remaining thread explicitly choose `keep_unresolved`. Then submit one
  aggregate summary. With the CLI, put
  `{"summary":"...","dispositions":[...]}` in a workspace-local JSON file and
  retry with `multica issue status <id> done --comment-review-file <file>`.
  Never resolve merely to clear the gate. A new reply changes the snapshot and
  requires another review; an unchanged explicit keep remains valid.
- **`cancelled`** is a terminal, user-driven decision to close the issue. Like
  `done` it enqueues no new agent work, but it does **not** stop tasks already in
  flight — a run in progress keeps going (MUL-4465). To stop a running task,
  cancel the task itself.
- **Failed issue-triggered tasks** may roll an issue from `in_progress` back to
  `todo` when no active task / retry remains — that is the main server-owned
  status write on the agent-run path.

## Commenting on one passage of the description

`multica issue comment add <id> --anchor "<passage verbatim>" --content "..."`
attaches a comment to specific words instead of to the issue. Copy the passage
exactly — a near-miss is a hard error, not a warning, because the alternative is
a comment that silently highlights nothing. Repeats are disambiguated with
`--anchor-occurrence`, never by widening the passage until it happens to be
unique. Details: `references/quoting-a-passage.md`.

## Comment threads: one question, one conclusion

A thread is a discussion. One atomic question per TOP-LEVEL comment; anything
ANSWERING an existing comment is a reply — write it with `--parent`, because a
comment can never be re-parented and two top-level comments where one settles
the other stay flat forever. A DIFFERENT question opens a new top-level comment.

When it concludes, resolve the comment that HOLDS the conclusion — resolving the
root when the conclusion is in a reply hides that conclusion from every default
read, because a root-resolved thread folds to the root alone.

Resolve at the end, not along the way: a new reply does not reopen a thread
whose conclusion is a reply, so replies added after that are invisible with
nothing warning anyone. Review ROUNDS are phases, not threads. Full rules and
the reopen gap: `references/comment-threads.md`.

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

## Fixing a comment you just wrote

```bash
multica issue comment edit <comment-id> --content "..."
```

For correcting your OWN comment moments after posting it — a typo, a wrong
number, a broken link. The new body REPLACES the old one, so `get` it first
when the fix touches only part of a long comment; a correction that ADDS
information belongs in a reply instead, where the discussion keeps its history.
Only the author and workspace admins may edit; attachments are left untouched.
Mind the mentions in the new body: an edit re-runs `@agent` / `@squad` triggers
(and cancels tasks the old body started), so an edit can hire an agent the
original never called.

## Reading one passage of a description

`multica issue get <id> --quote-start "..." --quote-end "..."` returns that span alone,
erroring rather than guessing when edges are ambiguous — locating a passage yourself costs
the whole body and can land on the wrong one. `references/quoting-a-passage.md`.

## Phases — filing what happened under the station it happened in

A phase is a container inside one issue holding the comments written while the
work was there — comment 3 and comment 30 otherwise arrive in one flat run with
nothing saying they belong to different stretches. Not a status: `status`
forgets the route, a phase stays.

```bash
multica issue comment add <id> --phase 评审 --content "..."
multica issue phase list <id>
```

**Every new issue is created with 需求梳理 → 方案评审 → 代码评审 → 测试验收 →
需求冻结 already on it** — file into
those rather than building a route first. `<phase>` is the NAME, matched
exact-first so `评审` still resolves once `评审 2` exists.

Three that bite: a reply with `--parent` and no `--phase` inherits its parent's
station; deleting a phase deletes its comments; and an issue predating the
feature has no route, which is **not a bug to fix** — drop the flag rather than
fabricating stations the work never took. Commands, the 409 rules, and how
activities are placed: `references/phases.md`.

## An issue that ended: frozen, archived, deleted

Once an issue is `done` or `cancelled`, its **title and description are frozen**
— the endpoint returns 409, and that covers `issue update` too. Everything else
still works: comments, status, labels, metadata, relationships. The lock opens
by leaving the terminal status, in a separate request from the rewrite. Found a
mistake in a finished issue? **Ask the user first** — never reopen on your own.

Reading one has its own rule, and it is the expensive one: a finished issue says
**what was true when it finished**, accurate about the past and silent about the
present. Do not treat its description as current state; do not discount it
either, because why a decision was made is usually written nowhere else. Nothing
points at whatever replaced it — check `superseded_by` in metadata.

**Archiving is a different dimension from status.** `status` says how the work
ended; archived says whether the card is still in view. Do NOT reach for
`cancelled` to get a shipped issue off the board — archive it, and it keeps the
status it ended on. Archiving takes the whole sub-issue subtree; **deleting does
not**, and `--force` orphans the children instead.

Full rules — the two-request reopen, superseded_by, subtree behaviour, and what
delete destroys: `references/issue-endings.md`.

## `issue create` assigns to the caller unless told otherwise

`multica issue create` with no `--assignee` / `--assignee-id` assigns the new
issue to the member behind the current token. Pass `--no-assignee` to leave it
unassigned.

This does not change anything for you: an agent token has no member identity,
so the default resolves to nothing and the issue is created unassigned exactly
as before. Keep passing `--assignee <agent>` when handing work to an agent —
that is still the only way the issue reaches one.

## `issue create` posts the card's index for you

Every card filed through `multica issue create` comes with its pinned root
index already posted — the `产物落点` / `当前状态` skeleton the team ledger rule
asks for. **Do not post a second one.** Update the existing one with `multica
issue comment edit <comment-id>`; the pin survives an edit.

The index records the session that filed the card, as a one-time snapshot taken
at creation. It comes from `CLAUDE_CODE_SESSION_ID` / `CODEX_SESSION_ID` — the
variables your runtime exports to the commands you spawn — so a card you file
carries your own session id without you passing anything. `--session <id>`
overrides it; with neither the line reads `未记录` and the card is still filed.

That line is a snapshot and stays as written. Code progress is the other thing,
it keeps moving, and it does not go in the index: leave it in the worktree,
where `worktree sync` measures it, and point at it with `multica worktree show
<name>`.

Posting the index is best effort. If it fails, the card still exists and the
CLI warns on stderr — post it by hand rather than re-running `issue create`,
which would file a duplicate card.

## Resources: a page that lives somewhere else

A design doc, a meeting note, a vendor page — anything whose home is outside
Multica but belongs next to this issue.

```bash
multica issue resource add COC-1 "https://example.feishu.cn/docx/abc" --title "沟通会纪要"
multica issue resource list COC-1
multica issue resource rename COC-1 <resource-id> "新标题"
multica issue resource remove COC-1 <resource-id>
```

The title is TYPED, never fetched — the documents worth attaching return a
login page to an anonymous request. Left out, the row reads as host and path.
http(s) only; the server rejects the rest, because the row is clickable.

Use this rather than a link in the description: a body link cannot be listed or
removed without editing prose, and a finished body is frozen so it could not be
added at all. A resource CAN be attached to a finished issue — filing something
next to a record is not editing it.

## Documents: writing that is not an issue

A retrospective, a lesson learned, an SOP you go back to is not work to be
tracked, and filing it as an issue gives it a status nobody will move. Write a
document: title plus Markdown, owned by the workspace, optionally linked to the
issue it came from.

```bash
multica wiki add --title "COC-97 踩坑" --content-stdin < notes.md
multica wiki list                       # newest first; CHARS says how long each is
multica wiki list --search 踩坑          # title AND body, whole workspace
multica wiki list --issue COC-97        # only documents linked to that issue
multica wiki get <doc-id>
```

Pipe the body in for anything longer than a sentence — inline `--content`
mangles newlines.

`--kind` is a folder PATH, not a flat label. Slashes make levels, and asking
for a folder returns everything below it:

```bash
multica wiki add --kind "本地联调/P0 workflow" --title "..." --content-stdin < sop.md
multica wiki list --kind 本地联调          # includes 本地联调/P0 workflow
multica wiki kinds                       # every kind that exists, nested ones included
```

A folder exists exactly as long as a document names it, so there is nothing to
create first — but read `wiki kinds` before inventing a path, because a
near-duplicate folder is worse than a wrong one: nobody looking in either finds
both.

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

Each section above points at the file carrying its full rules. In one place:

| File | Covers |
| --- | --- |
| `references/phases.md` | Stations, the 409 rules, where activities land |
| `references/comment-threads.md` | Thread shape, which comment to resolve, the reopen gap |
| `references/quoting-a-passage.md` | Reading one span of a description, and anchored comments |
| `references/issue-endings.md` | Frozen bodies, archiving vs status, what delete destroys |
| `references/pull-request-response.md` | Every field on a linked PR, and the checks snapshot |

`references/working-on-issues-source-map.md` — accurate `file:line` for every
contract above: the `pull-requests` CLI and route, the PR response field list,
`derivePRState`, the two-path link (`extractIdentifiers`) vs close-intent
(`extractClosingIdentifiers`) proof, the backlog enqueue lines, child-done
notify, the stage column / `stageBarrierClosed` barrier and the `--stage` /
`issue children` CLI, and the metadata CLI. Re-derive before depending on an
exact line.
