import { describe, it, expect } from "vitest";
import {
  WorktreeSchema,
  WorktreeListResponseSchema,
  WorktreeEntrySchema,
  WorktreeEntryListResponseSchema,
} from "./schemas";

// The ledger is what someone reads to decide which branch to work on and what
// the last session left half-done. A drifting backend must degrade a row, never
// empty the page — so the cases that matter here are the malformed ones.

describe("WorktreeSchema", () => {
  it("parses a well-formed tree", () => {
    const parsed = WorktreeSchema.parse({
      id: "w-1",
      workspace_id: "ws-1",
      name: "openwiki-suite",
      path: "/Users/x/code/multica",
      repo: "multica",
      branch: "feat/openwiki-suite",
      base_ref: "main",
      role: "feature",
      status: "active",
      head_sha: "82a655d60979000000000000000000000000abcd",
      merged_sha: "",
      merged_into: "",
      dirty: true,
      verified_at: "2026-08-20T12:00:00Z",
      session: {
        agent: "claude",
        resume: "claude --resume abc",
        owner: "cocoyu",
        next_action: "接前端",
        updated_at: "2026-08-20T12:00:00Z",
      },
      parent_id: null,
      entry_count: 3,
      created_at: "2026-08-20T00:00:00Z",
      updated_at: "2026-08-20T12:00:00Z",
    });
    expect(parsed.name).toBe("openwiki-suite");
    expect(parsed.session.agent).toBe("claude");
    expect(parsed.dirty).toBe(true);
  });

  // A backend that predates the session slot still describes a real tree. The
  // row renders with an empty slot rather than disappearing from the ledger.
  it("fills in a missing session slot and counts", () => {
    const parsed = WorktreeSchema.parse({
      id: "w-1",
      name: "consult-speedup",
    });
    expect(parsed.session.agent).toBe("");
    expect(parsed.session.updated_at).toBeNull();
    expect(parsed.entry_count).toBe(0);
    expect(parsed.role).toBe("feature");
    expect(parsed.status).toBe("active");
    expect(parsed.verified_at).toBeNull();
  });

  // Identity has no sensible default: a tree with no name cannot be addressed,
  // so it is not a degraded row, it is not a row.
  it("rejects a tree with no name", () => {
    expect(() => WorktreeSchema.parse({ id: "w-1" })).toThrow();
  });

  // A role or status this build has not heard of renders as itself. Dropping
  // the row would hide a branch that exists.
  it("keeps roles and statuses it does not recognise", () => {
    const parsed = WorktreeSchema.parse({ id: "w-1", name: "x", role: "hotfix", status: "paused" });
    expect(parsed.role).toBe("hotfix");
    expect(parsed.status).toBe("paused");
  });

  it("tolerates fields it has never seen", () => {
    const parsed = WorktreeSchema.parse({ id: "w-1", name: "x", stack_depth: 2 });
    expect(parsed.name).toBe("x");
  });
});

describe("WorktreeEntrySchema", () => {
  it("parses an entry and defaults its optional halves", () => {
    const parsed = WorktreeEntrySchema.parse({
      id: "e-1",
      worktree_id: "w-1",
      body: "改了 X，未提交",
      created_at: "2026-08-20T12:00:00Z",
    });
    expect(parsed.kind).toBe("progress");
    expect(parsed.sha).toBe("");
    expect(parsed.issue_id).toBeNull();
  });

  // Kinds are server-driven. A newer one must read through as itself rather
  // than fail the list, which is why the field is a string and not an enum.
  it("keeps an entry kind it does not recognise", () => {
    const parsed = WorktreeEntrySchema.parse({ id: "e-1", kind: "deploy", body: "shipped" });
    expect(parsed.kind).toBe("deploy");
  });
});

describe("worktree list responses", () => {
  it("defaults missing lists to empty rather than failing", () => {
    expect(WorktreeListResponseSchema.parse({}).worktrees).toEqual([]);
    expect(WorktreeEntryListResponseSchema.parse({}).entries).toEqual([]);
  });
});
