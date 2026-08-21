import { describe, it, expect, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    workspaceWiki: () => "/acme/workspace/wiki",
    workspaceSkills: () => "/acme/workspace/skills",
    workspaceInstructions: () => "/acme/workspace/instructions",
    workspaceWorktree: () => "/acme/workspace/worktree",
    workspaceAgentWiki: () => "/acme/workspace/agentwiki",
  }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

// The views' own contents are tested where they live. This file is about the
// shell around them, so each is a marker.
// Both wikis are this one page under opposite filters, so the marker says
// which filter it was mounted with.
vi.mock("@multica/views/docs", () => ({
  DocsPage: ({ hideKinds }: { hideKinds?: (kind: string) => boolean }) => (
    <div
      data-testid={hideKinds?.("AgentWiki/cases/") ? "wiki" : "agentwiki"}
    />
  ),
}));
vi.mock("@multica/views/skills", () => ({
  SkillsPage: () => <div data-testid="skills">skills</div>,
}));
vi.mock("./worktree-ledger", () => ({
  WorktreeLedger: () => <div data-testid="worktree">worktree</div>,
}));
vi.mock("./instructions-page", () => ({
  InstructionsPage: () => <div data-testid="instructions">instructions</div>,
}));


import { OpenwikiPage, type OpenwikiTab } from "./openwiki-page";

const TABS: OpenwikiTab[] = [
  "wiki",
  "skills",
  "instructions",
  "worktree",
  "agentwiki",
];

/**
 * The dashboard shell sets overflow-hidden, so a page that does not carry its
 * own scroll container simply loses everything below the fold — no scrollbar,
 * no keyboard scrolling, nothing to indicate there is more. That is invisible
 * to a render test unless asserted directly, and it shipped once.
 */
function scrollContainerOf(element: HTMLElement | null): HTMLElement | null {
  for (let node = element; node; node = node.parentElement) {
    if (node.className.includes("overflow-y-auto")) return node;
  }
  return null;
}

describe("OpenwikiPage", () => {
  it("renders the view its address names", () => {
    for (const tab of TABS) {
      const { unmount } = renderWithI18n(<OpenwikiPage tab={tab} />);
      expect(screen.getByTestId(tab)).toBeInTheDocument();
      unmount();
    }
  });

  it("gives the plain views a scroll container, and the self-scrolling ones none", () => {
    // Both wikis and skills scroll their own lists; a wrapper here would nest
    // a second scrollbar inside the first.
    for (const tab of ["wiki", "skills", "agentwiki", "instructions"] as OpenwikiTab[]) {
      const { unmount } = renderWithI18n(<OpenwikiPage tab={tab} />);
      expect(scrollContainerOf(screen.getByTestId(tab))).toBeNull();
      unmount();
    }

    for (const tab of ["worktree"] as OpenwikiTab[]) {
      const { unmount } = renderWithI18n(<OpenwikiPage tab={tab} />);
      const container = scrollContainerOf(screen.getByTestId(tab));
      expect(
        container,
        `the ${tab} view renders past the fold and has nothing to scroll it`,
      ).not.toBeNull();
      // A flex child scrolls only if it is allowed to be shorter than its
      // content; without min-h-0 it grows instead and the clip comes back.
      expect(container?.className).toContain("min-h-0");
      unmount();
    }
  });
});
