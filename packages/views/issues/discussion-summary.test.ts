import { describe, it, expect } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { summarizeDiscussion } from "./discussion-summary";

function comment(id: string, resolved = false): TimelineEntry {
  return {
    id,
    type: "comment",
    resolved_at: resolved ? "2026-08-13T00:00:00Z" : null,
  } as unknown as TimelineEntry;
}

function threads(
  ...specs: Array<{ root: TimelineEntry; replies?: TimelineEntry[] }>
) {
  return {
    groups: specs.map((s) => ({ type: "comment" as const, entries: [s.root] })),
    replies: new Map(specs.map((s) => [s.root.id, s.replies ?? []])),
  };
}

describe("summarizeDiscussion", () => {
  it("counts replies as comments", () => {
    const { groups, replies } = threads({
      root: comment("r1"),
      replies: [comment("a"), comment("b")],
    });
    expect(summarizeDiscussion(groups, replies).comments).toBe(3);
  });

  // Activity rows are not discussion. Counting them would make the number
  // climb every time someone changed a status.
  it("ignores activity groups", () => {
    const { groups, replies } = threads({ root: comment("r1") });
    const withActivity = [
      ...groups,
      { type: "activities" as const, entries: [comment("act")] },
    ];
    expect(summarizeDiscussion(withActivity, replies).comments).toBe(1);
  });

  it("counts a thread with no conclusion as open", () => {
    const { groups, replies } = threads({
      root: comment("r1"),
      replies: [comment("a")],
    });
    expect(summarizeDiscussion(groups, replies).openThreads).toBe(1);
  });

  // The conclusion is usually a REPLY — it is what the discussion arrived at,
  // not what opened it. Reading only the root would report almost every
  // finished discussion as still open.
  it("settles a thread whose conclusion is on a reply", () => {
    const { groups, replies } = threads({
      root: comment("r1"),
      replies: [comment("a"), comment("b", true)],
    });
    expect(summarizeDiscussion(groups, replies).openThreads).toBe(0);
  });

  it("settles a thread resolved at its root", () => {
    const { groups, replies } = threads({ root: comment("r1", true) });
    expect(summarizeDiscussion(groups, replies).openThreads).toBe(0);
  });

  it("counts open and settled threads separately", () => {
    const { groups, replies } = threads(
      { root: comment("r1"), replies: [comment("a", true)] },
      { root: comment("r2"), replies: [comment("b")] },
      { root: comment("r3") },
    );
    expect(summarizeDiscussion(groups, replies)).toEqual({
      comments: 5,
      openThreads: 2,
    });
  });

  it("reports zeroes for an issue with no comments", () => {
    expect(summarizeDiscussion([], new Map())).toEqual({
      comments: 0,
      openThreads: 0,
    });
  });

  // A root whose replies have not been collected yet must not crash or count
  // phantom replies.
  it("survives a thread missing from the replies map", () => {
    const groups = [{ type: "comment" as const, entries: [comment("r1")] }];
    expect(summarizeDiscussion(groups, new Map())).toEqual({
      comments: 1,
      openThreads: 1,
    });
  });
});
