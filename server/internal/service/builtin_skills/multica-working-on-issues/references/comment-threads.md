# Comment threads: one question, one conclusion

A comment thread is a DISCUSSION, not a log. Getting its shape right is what
makes the folded read below cheap and truthful; getting it wrong makes the
folded read lose information without saying so.

## The shape

```
root            ← one atomic question, as a TOP-LEVEL comment
  └ reply       ← round 1
  └ reply       ← round 2
  └ reply       ← the conclusion         → resolve THIS one
```

| Situation | What to do |
| --- | --- |
| Opening a question, a review, a proposal | New TOP-LEVEL comment |
| Answering, disagreeing, iterating on it | **Reply under that root** (`--parent`) |
| A DIFFERENT question | New top-level comment — never a reply |
| The discussion has concluded | Resolve the comment holding the conclusion |

### Answering means `--parent`, and the choice is permanent

If your comment responds to an existing one — a verdict on a review, adopting
or refuting its findings, a follow-up to its question — it is a REPLY. Write it
with `--parent <that comment id>`. A new top-level comment is for a question
nobody asked yet.

This is not a matter of tidiness. **A comment cannot be re-parented**:
`UpdateComment` accepts `content`, `attachment_ids` and `suppress_agent_ids`
and nothing else. Two top-level comments where one answers the other are flat
forever — no fold can pair them, no resolution can span them, and a later reader
sees two unrelated statements with no way to tell that the second settles the
first. Opening a top-level comment by reflex, because it is what the composer
offers, is how that happens.

It has already happened: on COC-145 a verification and the verdict adopting it
were both written top-level, and the link between them now exists only in the
prose of the second one.

One thread carries one atomic question. Three unrelated topics in one thread
cannot fold into anything meaningful, because folding keeps the root and one
conclusion and drops the middle.

## Resolve the comment that holds the conclusion

This is the part that is easy to get backwards, and getting it backwards
deletes the thing you were trying to keep:

| Where the conclusion is | Resolve | Folded read keeps |
| --- | --- | --- |
| In a reply | **that reply** | root + that reply |
| The root already says it, or the thread is a dead end worth no conclusion | **the root** | root only — every reply is dropped |

Resolving the ROOT when the conclusion sits in a reply hides the conclusion
from every default read.

## What resolving changes

A resolved thread folds on the complete-thread reads — the default `list`,
`--recent`, and `--thread` without `--tail` — collapsing to its root plus
conclusion with the dropped count reported on the root. `--full` returns
everything. `--since`, `--tail` and `--roots-only` never fold.

So the saving is real but bounded: a thread that is only a root, with no
middle, saves nothing at all. The point is not to resolve everything — it is to
resolve what has genuinely ended, so a reader is not paying for settled
argument.

## Resolve at the END, not along the way

A new reply does NOT reopen a thread whose conclusion is a reply. The
auto-reopen only fires when the ROOT itself carries `resolved_at`.

The consequence is sharp: resolve halfway through, keep talking, and the new
replies are invisible to every default read, with nothing warning anyone. To
genuinely reopen, unresolve the comment that actually carries `resolved_at`
first:

```bash
multica issue comment unresolve <the-resolved-comment-id>
```

## Rounds are phases, not threads

A thread carries one discussion to its end. A second review PASS over revised
work is not more of that discussion — it is a new one, and the issue's phases
already model it (`方案评审`, `方案评审 2`, `代码评审` …).

| Axis | Divides |
| --- | --- |
| phase | Review rounds over the work |
| thread | One atomic question |
| resolution | That question's current answer — a newer one replaces it |

A thread holds at most one resolution: resolving a second comment atomically
clears the first. That is correct for supersession, which is what a later round
usually is — "ship it" replacing "not yet, three fixes" is not two findings to
keep. When two conclusions must BOTH stand, they were two questions, and
belonged in two threads by the atomic-question rule.

## A reply is not a dispatch

This fork disables implicit comment routing, so replying to an agent's comment
does not hire it. Only an explicit `@agent` / `@squad` does, plus squad
leader/worker coordination. See `implicitCommentRoutingEnabled` in
`server/internal/handler/comment.go`.

Practically: a thread records a discussion, it does not guarantee the other
side comes back. When someone actually has to act, mention them.
