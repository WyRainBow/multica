import { describe, it, expect, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Card } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const listCardsForIssue = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { listCardsForIssue },
  dispatchReasonCode: () => undefined,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ docs: () => "/acme/docs" }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

import { IssueDocsSection } from "./issue-docs-section";

function doc(overrides: Partial<Card> = {}): Card {
  return {
    id: "doc-1",
    workspace_id: "ws-1",
    issue_id: "issue-1",
    author_type: "member",
    author_id: "u-1",
    title: "本地起 P0 workflow 全链路 SOP",
    content: "x".repeat(11674),
    kind: "联调",
    created_at: "2026-07-23T00:00:00Z",
    updated_at: "2026-07-23T00:00:00Z",
    ...overrides,
  } as unknown as Card;
}

function render(docs: Card[]) {
  listCardsForIssue.mockResolvedValue({ cards: docs });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueDocsSection issueId="issue-1" />
    </QueryClientProvider>,
  );
}

// A document could always name its issue; the issue could not see back. Half a
// link is a link you have to remember, which is what having a document store
// was supposed to fix.
describe("issue → its documents", () => {
  it("lists a linked document by title", async () => {
    render([doc()]);
    expect(
      await screen.findByText("本地起 P0 workflow 全链路 SOP"),
    ).toBeInTheDocument();
  });

  // The length is what decides whether to open it now — an 11k-character SOP
  // is not something you read mid-issue.
  it("says how long it is", async () => {
    render([doc()]);
    expect(await screen.findByText("11674 chars")).toBeInTheDocument();
  });

  // Titles only. An earlier version rendered whole bodies here, which put the
  // same text on two screens with no way to tell which one anyone had read.
  it("does not render the body", async () => {
    render([doc({ content: "SECRET BODY TEXT" })]);
    await screen.findByText("本地起 P0 workflow 全链路 SOP");
    expect(screen.queryByText("SECRET BODY TEXT")).not.toBeInTheDocument();
  });

  // Most issues need no document, and a permanent "no documents" heading would
  // be noise on all of them.
  it("renders nothing at all when there are none", async () => {
    const { container } = render([]);
    await new Promise((r) => setTimeout(r, 0));
    expect(container).toBeEmptyDOMElement();
  });
});
