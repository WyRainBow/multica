import type { TimelineEntry } from "@multica/core/types";

/**
 * What the activity header says about the discussion below it.
 *
 * Two numbers, and they answer different questions. `comments` is how much
 * there is to read. `openThreads` is how much is still ASKING something —
 * the only one of the two you can act on, and the reason a header count is
 * worth having at all: an issue with forty comments and nothing open is
 * finished reading, while one with four comments and three open threads is
 * waiting on someone.
 */
export interface DiscussionSummary {
  /** Every comment, replies included. A reply is a comment. */
  comments: number;
  /** Threads with no conclusion marked anywhere in them. */
  openThreads: number;
}

/** The grouped shape the timeline is already built into. */
interface Group {
  type: "comment" | "activities";
  entries: TimelineEntry[];
}

/**
 * Counts over the grouped timeline rather than the raw entry list, so it
 * reports whatever the reader is actually looking at: pick a station and the
 * numbers narrow with the list beneath them. A header disagreeing with the
 * rows under it makes both untrustworthy.
 *
 * A thread counts as settled when a conclusion is marked ANYWHERE in it, root
 * or reply. The conclusion is usually a reply — it is what the discussion
 * arrived at, not what opened it — so reading only the root would report
 * almost every finished discussion as still open.
 */
export function summarizeDiscussion(
  groups: readonly Group[],
  threadReplies: ReadonlyMap<string, TimelineEntry[]>,
): DiscussionSummary {
  let comments = 0;
  let openThreads = 0;

  for (const group of groups) {
    if (group.type !== "comment") continue;
    const root = group.entries[0];
    if (!root) continue;

    const replies = threadReplies.get(root.id) ?? [];
    comments += 1 + replies.length;

    const settled =
      !!root.resolved_at || replies.some((reply) => !!reply.resolved_at);
    if (!settled) openThreads += 1;
  }

  return { comments, openThreads };
}
