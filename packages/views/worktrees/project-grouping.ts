import type { Issue, Project, Worktree } from "@multica/core/types";

export type ProjectGroupKey =
  | { kind: "project"; projectId: string }
  | { kind: "unassigned" }
  | { kind: "cross-project"; projectIds: string[] };

export interface ProjectGroup {
  key: string;
  project: Project | null;
  projectIds: string[];
  trees: Worktree[];
}

function issueProjectIds(issues: Issue[]): string[] {
  return [...new Set(issues.map((issue) => issue.project_id).filter((id): id is string => id !== null && id !== ""))].sort();
}

function familyFor(tree: Worktree, treesById: Map<string, Worktree>): Worktree[] {
  const family: Worktree[] = [];
  const pending = [tree.id];
  const seen = new Set<string>();
  const children = new Map<string, string[]>();
  for (const candidate of treesById.values()) {
    if (candidate.parent_id === null) continue;
    const siblings = children.get(candidate.parent_id) ?? [];
    siblings.push(candidate.id);
    children.set(candidate.parent_id, siblings);
  }

  while (pending.length > 0) {
    const id = pending.pop();
    if (id === undefined || seen.has(id)) continue;
    const current = treesById.get(id);
    if (current === undefined) continue;
    seen.add(id);
    family.push(current);
    if (current.parent_id !== null) pending.push(current.parent_id);
    for (const child of children.get(id) ?? []) pending.push(child);
  }
  return family;
}

export function projectGroupKey(
  trees: Worktree[],
  issuesByTree: Map<string, Issue[]>,
  tree: Worktree,
): ProjectGroupKey {
  const treesById = new Map(trees.map((candidate) => [candidate.id, candidate]));
  const projectIds = [
    ...new Set(
      familyFor(tree, treesById).flatMap((candidate) =>
        issueProjectIds(issuesByTree.get(candidate.name) ?? []),
      ),
    ),
  ].sort();
  if (projectIds.length === 0) return { kind: "unassigned" };
  if (projectIds.length === 1) return { kind: "project", projectId: projectIds[0]! };
  return { kind: "cross-project", projectIds };
}

export function groupWorktreesByProject(
  trees: Worktree[],
  issuesByTree: Map<string, Issue[]>,
  projects: Project[],
): ProjectGroup[] {
  const projectById = new Map(projects.map((project) => [project.id, project]));
  const groups = new Map<string, ProjectGroup>();
  for (const tree of trees) {
    const key = projectGroupKey(trees, issuesByTree, tree);
    const groupKey = key.kind === "project" ? `project:${key.projectId}` : key.kind;
    let group = groups.get(groupKey);
    if (group === undefined) {
      group = {
        key: groupKey,
        project: key.kind === "project" ? projectById.get(key.projectId) ?? null : null,
        projectIds:
          key.kind === "project"
            ? [key.projectId]
            : key.kind === "cross-project"
              ? key.projectIds
              : [],
        trees: [],
      };
      groups.set(groupKey, group);
    }
    group.trees.push(tree);
  }

  return [...groups.values()].sort((a, b) => {
    if (a.key === "unassigned") return 1;
    if (b.key === "unassigned") return -1;
    if (a.key === "cross-project") return 1;
    if (b.key === "cross-project") return -1;
    return (a.project?.title ?? a.key).localeCompare(b.project?.title ?? b.key);
  });
}
