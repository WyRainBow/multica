# Phases: the stations an issue passes through

A long issue's comments arrive in one flat run — comment 3 and comment 30 can
belong to different stretches of work and nothing says so. A PHASE is a
container inside one issue holding the comments written while it was there.

Not a status: `status` forgets the route, a phase stays.

**Every new issue is created with 调研记录 → 需求梳理 → 方案评审 → 代码评审 →
测试验收 → 需求冻结 on it**, sub-issues included. Put research findings and
conclusions in the 调研记录 phase, not in a review phase. The two reviews are
separate on purpose:
方案评审 asks whether this is the right thing to build, 代码评审 whether it was
built right, and one combined station puts both answers in the same pile.

Either recurs — `方案评审 2`, `代码评审 2` — each its own station.

## Commands

```bash
multica issue phase list <id>                      # NAME, STATE, COMMENTS, ENTERED, COMPLETED
multica issue phase add <id> "方案评审 2"
multica issue phase enter <id> 代码评审                # record arrival
multica issue phase complete <id> 代码评审             # record departure
multica issue phase rename <id> <phase> <new-name>
multica issue phase delete <id> <phase>
multica issue comment add <id> --phase 代码评审 --content "..."
multica issue comment list <id> --phase 代码评审
```

## Rules that bite if you assume otherwise

**`<phase>` is the NAME** — case-insensitive, a unique prefix is enough, a full
UUID also works. Exact match is tried first, so `方案评审` still resolves once
`方案评审 2` exists.

**Names are unique per issue.** A duplicate returns 409 rather than leaving two
stations that read the same.

**`complete` requires `enter` first** (409 otherwise) — completing an unentered
phase records a route the work never took. `enter` on an already-entered phase
keeps the FIRST arrival and clears completion: coming back is not starting over.

**State is derived, never stored**: completed → `done`, entered → `current`,
neither → `pending`. A phase does not have to be entered to be filed into.

**A reply joins the comment it answers.** `--parent` with no `--phase` takes the
parent's station, so a thread never has to restate it; pass `--phase` on a reply
only to move it somewhere else.

**Deleting a phase deletes its comments**, so the CLI demands `--force` once it
holds any. `phase list` shows that count — it is the only warning there is.

**`comment list --phase` filters client-side**, so it is rejected together with
`--recent` / `--tail` / `--thread` / `--before`: those pick a window first, and
the result would read as "everything in this phase" while being a slice of it.

## Activities have no phase

Status changes and description edits carry no phase field. The UI places them by
TIME, into whichever station was current when they happened; the CLI does not
group them at all.

## Issues predating the feature have no route

**Not a bug to fix.** Adding stations an issue never used fabricates a history
rather than recovering one — add one only when someone is about to file into it.

`--phase` on such an issue fails with "add one with `multica issue phase add`".
That message is for a person deciding to start a route, not an instruction to
the agent that hit it. Drop the flag and comment without it.
