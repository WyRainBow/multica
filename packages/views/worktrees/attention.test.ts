import { describe, it, expect } from "vitest";
import type { Issue, Worktree } from "@multica/core/types";
import { attentionItems } from "./attention";

function tree(overrides: Partial<Worktree> = {}): Worktree {
  return {
    id: "w-1",
    workspace_id: "ws-1",
    name: "feat-1",
    path: "/repo",
    repo: "multica",
    branch: "feature/wy/COC-1/x",
    base_ref: "main",
    role: "feature",
    status: "active",
    head_sha: "",
    merged_sha: "",
    merged_into: "",
    dirty: false,
    verified_at: "2026-08-20T00:00:00Z",
    session: {
      agent: "claude",
      resume: "claude --resume x",
      owner: "cocoyu",
      next_action: "",
      updated_at: "2026-08-20T00:00:00Z",
    },
    parent_id: null,
    entry_count: 0,
    created_at: "2026-08-20T00:00:00Z",
    updated_at: "2026-08-20T00:00:00Z",
    ...overrides,
  };
}

function issue(status: string): Issue {
  return { id: "i-1", identifier: "COC-1", title: "t", status } as Issue;
}

const kinds = (items: ReturnType<typeof attentionItems>) =>
  items.map((i) => i.kind);

describe("attentionItems", () => {
  it("says nothing about a tree that is measured, claimed and clean", () => {
    expect(attentionItems([tree()], new Map())).toEqual([]);
  });

  it("raises uncommitted work above anything merely unrecorded", () => {
    const items = attentionItems(
      [
        tree({ id: "a", name: "a", verified_at: null, session: { ...tree().session, agent: "" } }),
        tree({ id: "b", name: "b", dirty: true }),
      ],
      new Map(),
    );
    expect(kinds(items)[0]).toBe("uncommitted");
  });

  it("catches a branch that landed while its cards stayed open", () => {
    const merged = tree({ status: "merged", merged_sha: "a".repeat(40) });
    const byTree = new Map([["feat-1", [issue("in_progress"), issue("done")]]]);

    const items = attentionItems([merged], byTree);
    const item = items.find((i) => i.kind === "merged_open_cards");
    expect(item, "a merged tree with an open card should be surfaced").toBeDefined();
    // Only the cards still open — listing the closed one would make the row
    // look like more work than it is.
    expect(item?.issues).toHaveLength(1);
  });

  it("says nothing when a merged tree's cards are all closed", () => {
    const merged = tree({ status: "merged", merged_sha: "a".repeat(40) });
    const byTree = new Map([["feat-1", [issue("done")]]]);
    expect(kinds(attentionItems([merged], byTree))).not.toContain(
      "merged_open_cards",
    );
  });

  it("leaves archived trees alone", () => {
    const archived = tree({ status: "archived", dirty: true, verified_at: null });
    expect(attentionItems([archived], new Map())).toEqual([]);
  });

  it("only calls a tree unclaimed while it is still active", () => {
    const noSession = { ...tree().session, agent: "" };
    expect(
      kinds(attentionItems([tree({ session: noSession })], new Map())),
    ).toContain("unclaimed");
    expect(
      kinds(
        attentionItems(
          [tree({ status: "merged", session: noSession })],
          new Map(),
        ),
      ),
    ).not.toContain("unclaimed");
  });
});
