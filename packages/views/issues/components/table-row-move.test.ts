import { describe, it, expect } from "vitest";
import {
  resolveRowDrop,
  rowDragId,
  rowDropZone,
  isSelfOrDescendant,
} from "./table-view";
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

describe("resolveRowDrop — reordering", () => {
  it("places a row dragged downwards after its drop target", () => {
    // a dropped on c: siblings without a are [b, c], so a lands below c.
    // `before_id` is the neighbour ABOVE, so it is c — not the row a used to
    // sit next to.
    expect(resolveRowDrop(rows, "a", "c", "after")).toEqual({
      kind: "reorder",
      issueId: "a",
      anchors: { before_id: "c", after_id: null },
    });
  });

  it("places a row dragged upwards before its drop target", () => {
    // c dropped on a: siblings without c are [a, b], so c lands before a.
    expect(resolveRowDrop(rows, "c", "a", "before")).toEqual({
      kind: "reorder",
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
    expect(resolveRowDrop(longer, "e", "b", "before")).toEqual({
      kind: "reorder",
      issueId: "e",
      anchors: { before_id: "a", after_id: "b" },
    });
  });

  it("refuses an edge drop onto a different parent", () => {
    // An edge says "next to this", which in another family names no parent.
    // Re-parenting is the middle band, and only the middle band.
    expect(resolveRowDrop(rows, "a", "d", "before")).toBeNull();
    expect(resolveRowDrop(rows, "a", "d", "after")).toBeNull();
  });

  it("refuses an edge drop from a child onto a top-level row", () => {
    expect(resolveRowDrop(rows, "a", "other-parent", "before")).toBeNull();
  });

  it("reorders top-level rows among themselves", () => {
    expect(resolveRowDrop(rows, "other-parent", "parent", "before")).toEqual({
      kind: "reorder",
      issueId: "other-parent",
      anchors: { before_id: null, after_id: "parent" },
    });
  });

  it("is a no-op when a row is dropped on itself", () => {
    expect(resolveRowDrop(rows, "a", "a", "nest")).toBeNull();
  });

  it("ignores ids that are not on screen", () => {
    // The table virtualizes and paginates; a stale drag id must not resolve to
    // a neighbour pair computed from a partial list.
    expect(resolveRowDrop(rows, "ghost", "a", "before")).toBeNull();
    expect(resolveRowDrop(rows, "a", "ghost", "nest")).toBeNull();
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
    expect(resolveRowDrop(withChrome, "b", "a", "before")).toEqual({
      kind: "reorder",
      issueId: "b",
      anchors: { before_id: null, after_id: "a" },
    });
  });

  it("refuses when the sibling list has nowhere to put the row", () => {
    const onlyChild = [issueRow("parent", null), issueRow("a", "parent")];
    expect(resolveRowDrop(onlyChild, "a", "a", "before")).toBeNull();
  });
});

describe("rowDropZone", () => {
  // A 40px row at y=100. The middle half nests; the outer quarters reorder.
  it("reads the top quarter as 'before'", () => {
    expect(rowDropZone(105, 100, 40)).toBe("before");
  });

  it("reads the bottom quarter as 'after'", () => {
    expect(rowDropZone(135, 100, 40)).toBe("after");
  });

  it("reads the middle half as 'nest'", () => {
    expect(rowDropZone(120, 100, 40)).toBe("nest");
    expect(rowDropZone(111, 100, 40)).toBe("nest");
    expect(rowDropZone(129, 100, 40)).toBe("nest");
  });

  // Nesting is the wider band on purpose: a drop is one gesture with no
  // confirmation, and re-parenting by accident moves the row out of the list
  // you were looking at. Aiming is required for the edges, not the middle.
  it("gives nesting the larger share of the row", () => {
    const zones = Array.from({ length: 40 }, (_, offset) =>
      rowDropZone(100 + offset, 100, 40),
    );
    const nesting = zones.filter((zone) => zone === "nest").length;
    expect(nesting).toBeGreaterThan(zones.length / 2);
  });

  // A row measured mid-layout has no height; nesting is the safe reading
  // because it is the zone the pointer is most likely in.
  it("treats a zero-height row as nesting", () => {
    expect(rowDropZone(100, 100, 0)).toBe("nest");
  });
});

describe("isSelfOrDescendant", () => {
  it("is true for the issue itself", () => {
    expect(isSelfOrDescendant(rows, "parent", "parent")).toBe(true);
  });

  it("is true for a direct child", () => {
    expect(isSelfOrDescendant(rows, "parent", "a")).toBe(true);
  });

  it("is true for a grandchild", () => {
    const deep = [
      issueRow("root", null),
      issueRow("mid", "root"),
      issueRow("leaf", "mid"),
    ];
    expect(isSelfOrDescendant(deep, "root", "leaf")).toBe(true);
  });

  it("is false for an unrelated row", () => {
    expect(isSelfOrDescendant(rows, "parent", "d")).toBe(false);
  });

  // Bad data must not hang the drag: a cycle terminates instead of looping.
  it("terminates on cyclic data", () => {
    const cyclic = [issueRow("x", "y"), issueRow("y", "x")];
    expect(isSelfOrDescendant(cyclic, "z", "x")).toBe(false);
  });
});

describe("resolveRowDrop — nesting", () => {
  // The feature: dropping into the middle of a row in another family makes
  // that row the parent.
  it("re-parents onto the row it was dropped into", () => {
    expect(resolveRowDrop(rows, "a", "other-parent", "nest")).toEqual({
      kind: "nest",
      issueId: "a",
      parentId: "other-parent",
      // Appended after the existing child: `before_id` is the neighbour ABOVE.
      anchors: { before_id: "d", after_id: null },
    });
  });

  it("nests into a row with no children yet", () => {
    expect(resolveRowDrop(rows, "d", "b", "nest")).toEqual({
      kind: "nest",
      issueId: "d",
      parentId: "b",
      // Nothing to sit next to; the server places it anywhere in the parent.
      anchors: { before_id: null, after_id: null },
    });
  });

  it("promotes a top-level row into another top-level row", () => {
    expect(resolveRowDrop(rows, "other-parent", "parent", "nest")).toEqual({
      kind: "nest",
      issueId: "other-parent",
      parentId: "parent",
      anchors: { before_id: "c", after_id: null },
    });
  });

  // Dropping a requirement inside its own subtree would make it its own
  // ancestor. The server rejects it too, but only after the optimistic patch
  // has reshaped the list.
  it("refuses nesting into its own child", () => {
    expect(resolveRowDrop(rows, "parent", "a", "nest")).toBeNull();
  });

  it("refuses nesting into its own grandchild", () => {
    const deep = [
      issueRow("root", null),
      issueRow("mid", "root"),
      issueRow("leaf", "mid"),
    ];
    expect(resolveRowDrop(deep, "root", "leaf", "nest")).toBeNull();
  });

  // Already there: the write would change nothing and still cost a request
  // and a realtime event on every client.
  it("refuses nesting into the parent it already has", () => {
    expect(resolveRowDrop(rows, "a", "parent", "nest")).toBeNull();
  });

  it("excludes the dragged row from its new siblings", () => {
    // b nested into parent — where it already is — is refused above; this
    // checks the anchor maths when moving between real families.
    const twoFamilies = [
      issueRow("p1", null),
      issueRow("x", "p1"),
      issueRow("p2", null),
      issueRow("y", "p2"),
      issueRow("z", "p2"),
    ];
    expect(resolveRowDrop(twoFamilies, "x", "p2", "nest")).toEqual({
      kind: "nest",
      issueId: "x",
      parentId: "p2",
      anchors: { before_id: "z", after_id: null },
    });
  });
});
