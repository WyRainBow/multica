import { describe, it, expect } from "vitest";
import {
  outlineDepths,
  activeOutlineId,
  type OutlineHeading,
} from "./outline";

const heading = (id: string, level: number): OutlineHeading => ({
  id,
  level,
  text: id,
  pos: Number(id.slice(1)),
});

describe("outlineDepths", () => {
  it("indents each level under the one before it", () => {
    const depths = outlineDepths([
      heading("h1", 1),
      heading("h2", 2),
      heading("h3", 3),
      heading("h4", 2),
    ]);
    expect(depths).toEqual([0, 1, 2, 1]);
  });

  it("does not indent a document that starts at h2", () => {
    // A description written entirely in ## / ### should sit flush left, not
    // indented by a level that is not in the document.
    expect(outlineDepths([heading("h1", 2), heading("h2", 3), heading("h3", 2)]))
      .toEqual([0, 1, 0]);
  });

  it("does not open a phantom tier when a level is skipped", () => {
    // h1 -> h3 is one step down for the reader, not two.
    expect(outlineDepths([heading("h1", 1), heading("h2", 3)])).toEqual([0, 1]);
  });

  it("returns to the outer level when a section closes", () => {
    expect(
      outlineDepths([
        heading("h1", 1),
        heading("h2", 3),
        heading("h3", 3),
        heading("h4", 1),
      ]),
    ).toEqual([0, 1, 1, 0]);
  });

  it("handles a flat document", () => {
    expect(outlineDepths([heading("h1", 2), heading("h2", 2)])).toEqual([0, 0]);
  });

  it("is empty for a document with no headings", () => {
    expect(outlineDepths([])).toEqual([]);
  });
});

describe("activeOutlineId", () => {
  const headings = [heading("a", 1), heading("b", 2), heading("c", 2)];
  const offsets = new Map([
    ["a", 0],
    ["b", 400],
    ["c", 900],
  ]);

  it("keeps the section highlighted after its heading scrolls away", () => {
    // The whole point: in a long section the heading leaves the viewport
    // while the reader is still inside it.
    expect(activeOutlineId(headings, offsets, 700)).toBe("b");
  });

  it("moves to the next section once its heading passes the reading line", () => {
    expect(activeOutlineId(headings, offsets, 950)).toBe("c");
  });

  it("highlights a heading exactly on the reading line", () => {
    expect(activeOutlineId(headings, offsets, 400)).toBe("b");
  });

  it("highlights nothing above the first heading", () => {
    // Reading the intro paragraph is not reading a section.
    expect(activeOutlineId(headings, new Map([["a", 100]]), 50)).toBeNull();
  });

  it("ignores headings whose position has not been measured", () => {
    // A heading added this frame has no offset yet; it must not blank out the
    // active state of the one the reader is actually under.
    expect(activeOutlineId(headings, new Map([["a", 0]]), 700)).toBe("a");
  });

  it("is null for an empty outline", () => {
    expect(activeOutlineId([], new Map(), 100)).toBeNull();
  });
});
