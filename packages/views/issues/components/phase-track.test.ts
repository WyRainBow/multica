import { describe, it, expect } from "vitest";
import type { IssuePhase } from "@multica/core/types";
import { phaseState } from "./phase-track";

function phase(overrides: Partial<IssuePhase> = {}): IssuePhase {
  return {
    id: "p-1",
    workspace_id: "ws-1",
    issue_id: "i-1",
    name: "实施中",
    position: 0,
    entered_at: null,
    completed_at: null,
    comment_count: 0,
    created_at: "2026-08-04T00:00:00Z",
    updated_at: "2026-08-04T00:00:00Z",
    ...overrides,
  };
}

// Derived from the two timestamps rather than stored. A stored state would be
// a third source of truth able to disagree with the times it summarizes.
describe("phaseState", () => {
  it("is pending before the issue arrives", () => {
    expect(phaseState(phase())).toBe("pending");
  });

  it("is current once entered", () => {
    expect(phaseState(phase({ entered_at: "2026-08-04T01:00:00Z" }))).toBe(
      "current",
    );
  });

  it("is done once completed", () => {
    expect(
      phaseState(
        phase({
          entered_at: "2026-08-04T01:00:00Z",
          completed_at: "2026-08-04T02:00:00Z",
        }),
      ),
    ).toBe("done");
  });

  // Completion wins over entry, so a station the work passed through does not
  // read as still current just because it was also entered.
  it("prefers completed over entered", () => {
    expect(
      phaseState(
        phase({
          entered_at: "2026-08-04T01:00:00Z",
          completed_at: "2026-08-04T02:00:00Z",
        }),
      ),
    ).toBe("done");
  });

  // Bad data — completed without ever being entered — still resolves to
  // something renderable rather than throwing in the middle of a header row.
  it("reads a completed-but-never-entered phase as done", () => {
    expect(phaseState(phase({ completed_at: "2026-08-04T02:00:00Z" }))).toBe(
      "done",
    );
  });
});
