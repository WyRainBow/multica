import { describe, it, expect } from "vitest";
import {
  activityAuthorName,
  formatDescriptionUpdate,
  mergeCoalescedDetails,
} from "./issue-detail";
import type { TimelineEntry } from "@multica/core/types";

// A translator that returns the key path plus its interpolations, so the tests
// assert on which sentence was chosen and with what numbers — not on wording.
const t = ((pick: (d: unknown) => unknown, vars?: Record<string, unknown>) => {
  const probe = new Proxy(
    {},
    {
      get: (_target, group: string) =>
        new Proxy({}, { get: (_t, key: string) => `${group}.${key}` }),
    },
  );
  const key = pick(probe) as string;
  return vars ? `${key}(${JSON.stringify(vars)})` : key;
}) as never;

function activity(details: Record<string, unknown>): TimelineEntry {
  return {
    type: "activity",
    id: "a1",
    actor_type: "member",
    actor_id: "u1",
    action: "description_updated",
    created_at: "2026-08-03T00:00:00Z",
    details,
  } as TimelineEntry;
}

describe("formatDescriptionUpdate", () => {
  it("reports how much changed", () => {
    expect(formatDescriptionUpdate(activity({ added_lines: 5, removed_lines: 2 }), t))
      .toContain("activity.description_updated_counts");
  });

  it("names the sections that changed", () => {
    const got = formatDescriptionUpdate(
      activity({ added_lines: 2, removed_lines: 1, sections: ["方案", "结论"] }),
      t,
    );
    expect(got).toContain("activity.description_updated_sections");
    expect(got).toContain("方案、结论");
  });

  it("collapses a long section list", () => {
    const got = formatDescriptionUpdate(
      activity({ added_lines: 9, removed_lines: 0, sections: ["A", "B", "C", "D"] }),
      t,
    );
    // The overflow phrase is nested inside the outer sentence, so its own
    // interpolations arrive JSON-escaped — assert on what it names, not on the
    // literal encoding.
    expect(got).toContain("activity.description_sections_more");
    expect(got).toContain("A、B");
    expect(got).not.toContain("C");
  });

  it("says the description was written, not edited, the first time", () => {
    // "+40 −0" reads oddly for the first time anyone wrote anything.
    expect(formatDescriptionUpdate(activity({ created: true, added_lines: 40 }), t))
      .toContain("activity.description_written");
  });

  it("says the description was cleared", () => {
    expect(formatDescriptionUpdate(activity({ cleared: true, removed_lines: 12 }), t))
      .toContain("activity.description_cleared");
  });

  it("falls back to the plain sentence for activities recorded before summaries existed", () => {
    // Old rows have `details = {}`. Rendering "+0 −0" would claim the edit
    // changed nothing, which is worse than saying less.
    expect(formatDescriptionUpdate(activity({}), t)).toBe("activity.description_updated");
  });
});

describe("mergeCoalescedDetails", () => {
  const merge = (a: Record<string, unknown>, b: Record<string, unknown>) =>
    mergeCoalescedDetails(activity(a), activity(b)) as Record<string, unknown>;

  it("sums the line counts across a collapsed run", () => {
    // Keeping only the newest entry's numbers would show one edit's counts
    // beside an "x2" badge.
    expect(merge({ added_lines: 3, removed_lines: 1 }, { added_lines: 2, removed_lines: 4 }))
      .toMatchObject({ added_lines: 5, removed_lines: 5 });
  });

  it("unions the changed sections without repeating one", () => {
    expect(
      merge({ sections: ["方案"] }, { sections: ["方案", "结论"] }).sections,
    ).toEqual(["方案", "结论"]);
  });

  it("keeps `created` from the oldest edit and `cleared` from the newest", () => {
    // Both describe the collapsed window as a whole, but from opposite ends.
    expect(merge({ created: true }, { removed_lines: 2 })).toMatchObject({ created: true });
    expect(merge({ added_lines: 2 }, { cleared: true })).toMatchObject({ cleared: true });
    expect(merge({ cleared: true }, { added_lines: 2 }).cleared).toBeUndefined();
  });

  it("omits sections entirely when neither edit touched one", () => {
    expect(merge({ added_lines: 1 }, { added_lines: 1 }).sections).toBeUndefined();
  });

  it("leaves other actions' details alone", () => {
    // Only description_updated has anything to merge; the badge has always
    // meant "newest entry's details" for everything else.
    const previous = { ...activity({ from: "todo" }), action: "status_changed" } as TimelineEntry;
    const next = { ...activity({ from: "in_progress" }), action: "status_changed" } as TimelineEntry;
    expect(mergeCoalescedDetails(previous, next)).toEqual({ from: "in_progress" });
  });
});

describe("activityAuthorName", () => {
  // The row Claude wrote used the member's token, so its actor is the member —
  // right for permissions, wrong for reading it back a week later.
  it("names the harness when the row records one", () => {
    expect(activityAuthorName(activity({ harness: "claude-code" }), "cocoyu")).toBe(
      "Claude",
    );
  });

  it("names Codex too", () => {
    expect(activityAuthorName(activity({ harness: "codex" }), "cocoyu")).toBe(
      "Codex",
    );
  });

  it("names the member when a person made the edit", () => {
    expect(activityAuthorName(activity({}), "cocoyu")).toBe("cocoyu");
  });

  // The header is client-controlled, so an unrecognised value must not print
  // itself into the timeline.
  it("falls back to the member for an unknown harness", () => {
    expect(
      activityAuthorName(activity({ harness: "definitely-not-real" }), "cocoyu"),
    ).toBe("cocoyu");
  });

  it("falls back when details are missing entirely", () => {
    const entry = { ...activity({}), details: undefined } as never;
    expect(activityAuthorName(entry, "cocoyu")).toBe("cocoyu");
  });
});
