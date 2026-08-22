import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue, Project, Worktree } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";

const listWorktrees = vi.hoisted(() => vi.fn());
const listIssues = vi.hoisted(() => vi.fn());
const listProjects = vi.hoisted(() => vi.fn());

/** issueListOptions fans out one request per status bucket. */
const issuesPage = (issues: Issue[]) => ({ issues, total: issues.length });

vi.mock("@multica/core/api", () => ({
  api: { listWorktrees, listIssues, listProjects },
  dispatchReasonCode: () => undefined,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

// The entry list fetches per tree; this file is about grouping, not the log.
vi.mock("./worktree-entry-list", () => ({
  WorktreeEntryList: () => <div />,
}));

import { WorktreeLedger } from "./worktree-ledger";

function tree(overrides: Partial<Worktree> = {}): Worktree {
  return {
    id: "w-1",
    workspace_id: "ws-1",
    name: "feat-coc295",
    path: "/repo",
    repo: "multica",
    branch: "feature/wy/COC-295/openwiki-tab",
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

const boundIssue = {
  id: "i-1",
  identifier: "COC-295",
  title: "openwiki tab",
  status: "done",
  project_id: "p-1",
  metadata: { "git.worktree": "feat-coc295" },
} as unknown as Issue;

const project = { id: "p-1", title: "Multica 优化" } as unknown as Project;

function render() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const result = renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <WorktreeLedger />
    </QueryClientProvider>,
  );
  return { ...result, queryClient };
}

/**
 * Wait until one source has answered while others are still in flight.
 *
 * Watching the DOM instead would be circular: a correct page renders nothing
 * during that window, which is exactly the state under test.
 */
async function waitForSettledQueries(queryClient: QueryClient, count: number) {
  await waitFor(() => {
    const settled = queryClient
      .getQueryCache()
      .getAll()
      .filter((query) => query.state.status === "success");
    expect(settled.length).toBeGreaterThanOrEqual(count);
  });
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("WorktreeLedger project grouping", () => {
  it("waits for issues and projects before claiming a tree has no project", async () => {
    // The race that shipped: worktrees answer first, and for as long as the
    // other two are in flight every tree looks unassigned — the page stated,
    // for about five seconds, the one thing the card was opened to check.
    // Worktrees land first and the other two stay in flight — the exact
    // ordering the page shipped with.
    listWorktrees.mockResolvedValue([tree()]);
    let releaseIssues: (value: Issue[]) => void = () => {};
    let releaseProjects: (value: Project[]) => void = () => {};
    listIssues.mockReturnValue(
      new Promise((resolve) => {
        releaseIssues = (issues) => resolve(issuesPage(issues));
      }),
    );
    listProjects.mockReturnValue(
      new Promise((resolve) => {
        releaseProjects = (projects) => resolve({ projects, total: projects.length });
      }),
    );

    const { queryClient } = render();

    await waitForSettledQueries(queryClient, 1);

    // Worktrees have landed; the other two have not. Nothing may be said about
    // project membership yet — not even "unassigned", which is a claim.
    expect(screen.getByText("Loading…")).toBeInTheDocument();
    expect(screen.queryByText("Unassigned project")).not.toBeInTheDocument();
    expect(screen.queryByText("feat-coc295")).not.toBeInTheDocument();

    releaseIssues([boundIssue]);
    releaseProjects([project]);

    // Once every source has answered, the tree lands under its real project.
    await waitFor(() => {
      expect(screen.getByText("Multica 优化")).toBeInTheDocument();
    });
    expect(screen.queryByText("Unassigned project")).not.toBeInTheDocument();
  });

  it("says unassigned only when the bound cards genuinely carry no project", async () => {
    listWorktrees.mockResolvedValue([tree()]);
    listIssues.mockResolvedValue(issuesPage([{ ...boundIssue, project_id: null } as Issue]));
    listProjects.mockResolvedValue({ projects: [project], total: 1 });

    render();

    await waitFor(() => {
      expect(screen.getByText("Unassigned project")).toBeInTheDocument();
    });
  });
});

