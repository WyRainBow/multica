import { describe, it, expect } from "vitest";
import { resolveRowMove, rowDragId } from "./table-view";
import type { Issue } from "@multica/core/types";

function issueRow(id: string, parentId: string | null) {
  return {
    kind: "issue" as const,
    key: id,
    issue: { id, parent_issue_id: parentId } as Issue,
    depth: parentId ? 1 : 0,
    hasChildren: false,
    collapsed: false,
  };
}

// parent
//  ├── a
//  ├── b
//  └── c
// other-parent
//  └── d
const rows = [
  issueRow("parent", null),
  issueRow("a", "parent"),
  issueRow("b", "parent"),
  issueRow("c", "parent"),
  issueRow("other-parent", null),
  issueRow("d", "other-parent"),
];

describe("rowDragId", () => {
  it("namespaces row ids away from column keys", () => {
    // Columns and rows share one DndContext; a bare uuid and a bare column key
    // like "status" live in the same id space without this.
    expect(rowDragId("abc")).not.toBe("abc");
    expect(rowDragId("abc")).toContain("abc");
  });
});

describe("resolveRowMove", () => {
  it("places a row dragged downwards after its drop target", () => {
    // a dropped on c: siblings without a are [b, c], so a lands below c.
    // `before_id` is the neighbour ABOVE, so it is c — not the row a used to
    // sit next to.
    expect(resolveRowMove(rows, "a", "c")).toEqual({
      issueId: "a",
      anchors: { before_id: "c", after_id: null },
    });
  });

  it("places a row dragged upwards before its drop target", () => {
    // c dropped on a: siblings without c are [a, b], so c lands before a.
    expect(resolveRowMove(rows, "c", "a")).toEqual({
      issueId: "c",
      anchors: { before_id: null, after_id: "a" },
    });
  });

  it("lands between both neighbours in the middle of a list", () => {
    const longer = [
      issueRow("parent", null),
      issueRow("a", "parent"),
      issueRow("b", "parent"),
      issueRow("c", "parent"),
      issueRow("e", "parent"),
    ];
    expect(resolveRowMove(longer, "e", "b")).toEqual({
      issueId: "e",
      anchors: { before_id: "a", after_id: "b" },
    });
  });

  it("refuses a drop onto a different parent", () => {
    // Re-parenting and repositioning have different consequences — a new
    // parent's assignee can be woken. Doing only one of the two silently
    // would be worse than refusing.
    expect(resolveRowMove(rows, "a", "d")).toBeNull();
  });

  it("refuses a drop from a child onto a top-level row", () => {
    expect(resolveRowMove(rows, "a", "other-parent")).toBeNull();
  });

  it("reorders top-level rows among themselves", () => {
    expect(resolveRowMove(rows, "other-parent", "parent")).toEqual({
      issueId: "other-parent",
      anchors: { before_id: null, after_id: "parent" },
    });
  });

  it("is a no-op when a row is dropped on itself", () => {
    expect(resolveRowMove(rows, "a", "a")).toBeNull();
  });

  it("ignores ids that are not on screen", () => {
    // The table virtualizes and paginates; a stale drag id must not resolve to
    // a neighbour pair computed from a partial list.
    expect(resolveRowMove(rows, "ghost", "a")).toBeNull();
    expect(resolveRowMove(rows, "a", "ghost")).toBeNull();
  });

  it("ignores non-issue rows when computing neighbours", () => {
    // Group headers and load-more rows sit in the same array; counting them as
    // siblings would send the server an anchor that is not an issue.
    const withChrome = [
      { kind: "group" as const, key: "g1" } as never,
      issueRow("parent", null),
      issueRow("a", "parent"),
      { kind: "load-more" as const, key: "lm" } as never,
      issueRow("b", "parent"),
    ];
    expect(resolveRowMove(withChrome, "b", "a")).toEqual({
      issueId: "b",
      anchors: { before_id: null, after_id: "a" },
    });
  });

  it("refuses when the sibling list has nowhere to put the row", () => {
    const onlyChild = [issueRow("parent", null), issueRow("a", "parent")];
    expect(resolveRowMove(onlyChild, "a", "a")).toBeNull();
  });
});
