import { describe, expect, it } from "vitest";
import type { Issue, Worktree } from "@multica/core/types";
import { groupWorktreesByProject, projectGroupKey } from "./project-grouping";

function tree(id: string, parentId: string | null = null): Worktree {
  return {
    id,
    workspace_id: "ws-1",
    name: id,
    path: `/tmp/${id}`,
    repo: "multica",
    branch: id,
    base_ref: "main",
    role: "feature",
    status: "active",
    head_sha: "",
    merged_sha: "",
    merged_into: "",
    dirty: false,
    verified_at: null,
    session: { agent: "", resume: "", owner: "", next_action: "", updated_at: null },
    parent_id: parentId,
    entry_count: 0,
    created_at: "2026-08-20T00:00:00Z",
    updated_at: "2026-08-20T00:00:00Z",
  };
}

function issue(identifier: string, projectId: string | null): Issue {
  return { id: identifier, identifier, title: identifier, status: "todo", project_id: projectId } as Issue;
}

describe("project grouping", () => {
  it("propagates one descendant project through the whole branch family", () => {
    const root = tree("root");
    const child = tree("child", root.id);
    const grandchild = tree("grandchild", child.id);
    const trees = [root, child, grandchild];
    const issues = new Map([[grandchild.name, [issue("COC-1", "project-1")]]]);

    expect(projectGroupKey(trees, issues, root)).toEqual({ kind: "project", projectId: "project-1" });
    expect(groupWorktreesByProject(trees, issues, [])).toHaveLength(1);
    expect(groupWorktreesByProject(trees, issues, [])[0]?.trees).toEqual(trees);
  });

  it("keeps a family without project-bound issues unassigned", () => {
    const trees = [tree("root"), tree("child", "root")];
    expect(projectGroupKey(trees, new Map(), trees[0]!)).toEqual({ kind: "unassigned" });
  });

  it("does not guess when a family contains multiple projects", () => {
    const root = tree("root");
    const child = tree("child", root.id);
    const trees = [root, child];
    const issues = new Map([
      [root.name, [issue("COC-1", "project-1")]],
      [child.name, [issue("COC-2", "project-2")]],
    ]);
    expect(projectGroupKey(trees, issues, root)).toEqual({
      kind: "cross-project",
      projectIds: ["project-1", "project-2"],
    });
    expect(groupWorktreesByProject(trees, issues, [])).toHaveLength(1);
    expect(groupWorktreesByProject(trees, issues, [])[0]?.key).toBe("cross-project");
    expect(groupWorktreesByProject(trees, issues, [])[0]?.projectIds).toEqual([
      "project-1",
      "project-2",
    ]);
  });

  it("keeps a missing project in the project bucket", () => {
    const trees = [tree("root")];
    const issues = new Map([["root", [issue("COC-1", "missing-project")]]]);
    const groups = groupWorktreesByProject(trees, issues, []);

    expect(groups[0]?.key).toBe("project:missing-project");
    expect(groups[0]?.projectIds).toEqual(["missing-project"]);
  });
});
