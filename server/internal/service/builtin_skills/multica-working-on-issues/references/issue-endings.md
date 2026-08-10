# When an issue ends: frozen, archived, deleted

Three different things, easy to reach for the wrong one.

## The body freezes at `done` / `cancelled`

`PUT /api/issues/{id}` refuses any request carrying `title` or `description` and
returns **409**. That covers `multica issue update --title/--description` too —
the CLI goes through the same endpoint.

A finished issue records what was true when it finished. The description is a
single current value, not a history, so a later edit leaves no way to tell which
version anyone acted on.

Everything else still works, deliberately: comments, status, archiving, labels,
metadata, reactions, relationships. A late sibling finishing still posts its
system comment on a done parent.

**The lock opens by leaving the terminal status.** The check reads the issue's
CURRENT status, so `done → in_progress` carries no body field and passes; the
body is writable from the next request on. One request that both reopens and
rewrites is still refused — do it in two.

Found a mistake in a finished issue? **Ask the user first**, then either reopen →
correct → close again, or file the correction as a new issue. Never reopen on
your own initiative.

## Reading one: a record, not the current state

The freeze is a rule about writing. Reading has its own, and it is the one that
costs you: a finished issue describes **what was true when it finished** —
accurate about the past, silent about the present. A design it describes may
have been replaced last month, and it neither knows that nor says so.

**Do not treat the description as current state.** Verify against the code, the
config, or a live check. `multica issue get` prints this warning on stderr.

**Do not discount it either.** Why a decision was made and what was rejected is
usually written down nowhere else and is still true. "Finished" is not "wrong";
skimming past a closed issue re-litigates settled decisions.

**There is no automatic pointer to whatever replaced it.** Relations are only
`blocks` / `blocked_by` / `related` plus parent/child — none of them means
"superseded by". A recorded successor is a metadata key, so check it, and record
it when finishing an issue that replaces an older one:

```bash
multica issue metadata list <id>                       # look for superseded_by
multica issue metadata set <old-id> --key superseded_by --value COC-99
```

## Archiving is a different dimension from status

| | Answers |
| --- | --- |
| `status` | how the work ended — `done`, `cancelled`, `blocked` |
| archived | whether the card should still be in view |

**Do NOT reach for `cancelled` to get a finished issue off the board.**
Cancelling records that the work was abandoned, which is wrong for something
that shipped, and the distinction is unrecoverable afterwards. Archive it — the
issue keeps whatever status it ended on.

```bash
multica issue archive <id>      # takes the issue AND its sub-issue subtree out of view
multica issue unarchive <id>    # brings the same subtree back
multica issue list --include-archived
```

**The whole subtree moves.** Archiving a requirement archives every sub-issue
under it, at any depth. Archive a mid-tree node and only that node's own subtree
goes; ancestors are untouched.

**Archived issues stay readable.** `issue get <id>` still returns one; only
list/board surfaces hide it, and links from other issues keep working.

Re-archiving an already-archived issue is a 409, not a silent no-op — the
original `archived_at` is preserved so "when did this leave the board" stays
answerable.

## Deleting is not archiving

```bash
multica issue delete <id>           # permanent; refuses if the issue has sub-issues
multica issue delete <id> --force   # deletes it anyway, ORPHANING its sub-issues
```

Delete destroys the issue with its comments, reactions and attachments, and it
cannot be undone. Reach for `archive` unless the issue should genuinely stop
existing.

The consequence worth knowing before `--force`: sub-issues are **not** deleted
with their parent. The parent link is `ON DELETE SET NULL`, so they survive as
top-level issues with no parent. `delete` refuses by default when children exist
precisely so that becomes a decision rather than something discovered
afterwards. If a whole tree should go out of view, archive it — archiving takes
the subtree, deleting does not.
