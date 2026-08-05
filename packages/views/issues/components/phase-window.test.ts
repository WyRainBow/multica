import { describe, it, expect } from "vitest";
import type { IssuePhase } from "@multica/core/types";
import { phaseAtTime } from "./phase-window";

function phase(
  name: string,
  entered: string | null,
  completed: string | null = null,
): IssuePhase {
  return {
    id: name,
    workspace_id: "ws-1",
    issue_id: "i-1",
    name,
    position: 0,
    entered_at: entered,
    completed_at: completed,
    comment_count: 0,
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
  };
}

const route = [
  phase("开始", "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z"),
  phase("已冻结", "2026-08-02T00:00:00Z", "2026-08-03T00:00:00Z"),
  phase("实施中", "2026-08-03T00:00:00Z"),
  phase("等待部署", null),
];

describe("phaseAtTime", () => {
  it("places an event inside the station that was current", () => {
    expect(phaseAtTime(route, "2026-08-02T12:00:00Z")?.name).toBe("已冻结");
  });

  // An unfinished station is still current, so its window has no end.
  it("places a recent event in the station still running", () => {
    expect(phaseAtTime(route, "2026-08-04T12:00:00Z")?.name).toBe("实施中");
  });

  // Real activity predates the route on any issue that gained phases later —
  // which is every existing issue.
  it("returns null for an event before the first station was entered", () => {
    expect(phaseAtTime(route, "2026-07-30T00:00:00Z")).toBeNull();
  });

  it("ignores stations never entered", () => {
    expect(phaseAtTime([phase("等待部署", null)], "2026-08-04T00:00:00Z")).toBeNull();
  });

  // A gap is honest: nothing was current, so the event belongs to no station.
  it("returns null inside a gap between stations", () => {
    const gapped = [
      phase("开始", "2026-08-01T00:00:00Z", "2026-08-02T00:00:00Z"),
      phase("实施中", "2026-08-05T00:00:00Z"),
    ];
    expect(phaseAtTime(gapped, "2026-08-03T00:00:00Z")).toBeNull();
  });

  // Re-entering a station clears its completion, so two can be open at once.
  // The one entered most recently is where the work actually is.
  it("prefers the most recently entered when windows overlap", () => {
    const overlapping = [
      phase("已冻结", "2026-08-01T00:00:00Z"),
      phase("实施中", "2026-08-03T00:00:00Z"),
    ];
    expect(phaseAtTime(overlapping, "2026-08-04T00:00:00Z")?.name).toBe("实施中");
  });

  it("includes the instant a station was entered", () => {
    expect(phaseAtTime(route, "2026-08-02T00:00:00Z")?.name).toBe("已冻结");
  });

  it("returns null for an unreadable timestamp", () => {
    expect(phaseAtTime(route, "not a date")).toBeNull();
  });
});
