import { describe, it, expect } from "vitest";
import type { IssuePhase } from "@multica/core/types";
import { nextRoundName } from "./phase-round";

function phase(name: string): IssuePhase {
  return {
    id: name,
    workspace_id: "ws-1",
    issue_id: "i-1",
    name,
    position: 0,
    entered_at: null,
    completed_at: null,
    comment_count: 0,
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
  };
}

describe("nextRoundName", () => {
  // Numbering starts at 2: a route that only ever reviews once should not show
  // a "1" implying a sequel that never came.
  it("names the second round 2", () => {
    expect(nextRoundName([phase("开始"), phase("评审")], "评审")).toBe("评审 2");
  });

  it("continues past the highest round", () => {
    expect(
      nextRoundName([phase("评审"), phase("评审 2"), phase("评审 3")], "评审"),
    ).toBe("评审 4");
  });

  // Asking for another round while looking at "评审 2" continues the sequence
  // rather than starting "评审 2 2".
  it("strips a round number off the base", () => {
    expect(nextRoundName([phase("评审"), phase("评审 2")], "评审 2")).toBe("评审 3");
  });

  it("uses the plain name when no round exists yet", () => {
    expect(nextRoundName([phase("开始")], "评审")).toBe("评审");
  });

  // Gaps happen when a middle round is deleted; the next one must not collide
  // with the highest that remains.
  it("does not reuse a number after a gap", () => {
    expect(nextRoundName([phase("评审"), phase("评审 3")], "评审")).toBe("评审 4");
  });

  // A different station that merely starts with the same letters is not a
  // round of it.
  it("ignores a station that only shares a prefix", () => {
    expect(nextRoundName([phase("评审"), phase("评审意见汇总")], "评审")).toBe(
      "评审 2",
    );
  });

  it("works for any station, not just review", () => {
    expect(nextRoundName([phase("实施")], "实施")).toBe("实施 2");
  });
});
