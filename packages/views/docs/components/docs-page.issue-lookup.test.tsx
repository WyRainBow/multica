import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Card, Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const listCards = vi.hoisted(() => vi.fn());
const listIssues = vi.hoisted(() => vi.fn());
const getIssue = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { listCards, listIssues, getIssue },
  dispatchReasonCode: () => undefined,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    docs: () => "/acme/docs",
    docDetail: (id: string) => `/acme/docs/${id}`,
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
  AppLink: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("../../rich-content", () => ({
  RichContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

vi.mock("./doc-editor-dialog", () => ({ DocEditorDialog: () => null }));

// The workspace list query fans out into paging helpers; this test is about
// what happens to the ids it does NOT return, so it stands in as empty.
vi.mock("@multica/core/issues/queries", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/issues/queries")>(
      "@multica/core/issues/queries",
    );
  return {
    ...actual,
    issueListOptions: () => ({
      queryKey: ["issues", "ws-1", "list"],
      queryFn: () => listIssues(),
    }),
  };
});

import { DocsPage } from "./docs-page";

const DOC = {
  id: "doc-1",
  workspace_id: "ws-1",
  issue_id: "issue-23",
  title: "模型主导编排 2026-08 建设复盘",
  content: "本文射程：……",
  kind: "复盘",
  created_at: "2026-08-13T03:28:00Z",
  updated_at: "2026-08-13T03:28:00Z",
} as unknown as Card;

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DocsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listCards.mockResolvedValue({ cards: [DOC], total: 1 });
  // Empty on purpose: fetchFirstPages returns the first pages only, and a
  // finished issue is routinely not among them.
  listIssues.mockResolvedValue([]);
  getIssue.mockReset();
});

// The row said "the linked issue is gone" about COC-23, an issue that exists
// and is done. It was never gone — it was simply not on the page of issues
// this view had loaded.
describe("a linked issue the workspace list did not return", () => {
  it("fetches it by id and shows its key", async () => {
    getIssue.mockResolvedValue({
      id: "issue-23",
      identifier: "COC-23",
      status: "done",
    } as unknown as Issue);

    render();

    expect(await screen.findByText("COC-23")).toBeInTheDocument();
    await waitFor(() => expect(getIssue).toHaveBeenCalledWith("issue-23"));
  });

  it("does not call it gone while the fetch is in flight", async () => {
    getIssue.mockImplementation(() => new Promise(() => {}));
    render();

    await screen.findByText("模型主导编排 2026-08 建设复盘");
    expect(
      screen.queryByText("The linked issue is gone"),
    ).not.toBeInTheDocument();
  });

  // A document whose issue really was deleted still has to say so, or the link
  // just disappears with no explanation.
  it("says gone once the fetch actually fails", async () => {
    getIssue.mockRejectedValue(new Error("404"));
    render();

    expect(
      await screen.findByText("The linked issue is gone"),
    ).toBeInTheDocument();
  });
});
