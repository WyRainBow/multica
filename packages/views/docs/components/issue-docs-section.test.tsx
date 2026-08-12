import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Card } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const listCardsForIssue = vi.hoisted(() => vi.fn());
const listCards = vi.hoisted(() => vi.fn());
const updateMutate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { listCardsForIssue, listCards },
  dispatchReasonCode: () => undefined,
}));

vi.mock("@multica/core/docs/mutations", () => ({
  useUpdateCard: () => ({ mutate: updateMutate }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    docs: () => "/acme/docs",
    docDetail: (id: string) => `/acme/docs/${id}`,
  }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    children,
    href,
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
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

function render(docs: Card[], pickable: Card[] = []) {
  listCardsForIssue.mockResolvedValue({ cards: docs });
  listCards.mockResolvedValue({ cards: pickable, total: pickable.length });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueDocsSection issueId="issue-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  updateMutate.mockClear();
});

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

  // It used to render nothing when empty, on the theory that a permanent "no
  // documents" line was noise. But an invisible section cannot be told apart
  // from a missing feature — and this one was invisible on every issue for as
  // long as the app had no way to create the link at all. It now shows, the way
  // the resources section directly above it does.
  it("still shows the section when there are none", async () => {
    render([]);
    expect(await screen.findByText("No documents yet.")).toBeInTheDocument();
  });

  // The count is a fact about a non-empty list; "0" next to an empty-state
  // sentence says the same thing twice.
  it("omits the count when empty", async () => {
    render([]);
    await screen.findByText("No documents yet.");
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });
});

// Attaching used to work only from the document. But which end you are standing
// on is not something the app gets to decide: reading an issue and remembering
// the SOP that belongs to it is as common as the reverse.
describe("attaching from the issue", () => {
  it("offers the link control even with nothing attached", async () => {
    render([]);
    expect(
      await screen.findByRole("button", { name: "Link a document" }),
    ).toBeInTheDocument();
  });

  it("picking a document writes this issue's id onto it", async () => {
    const user = userEvent.setup();
    render([], [doc({ id: "doc-9", title: "联调 SOP", issue_id: null })]);
    await user.click(
      await screen.findByRole("button", { name: "Link a document" }),
    );
    await user.click(await screen.findByText("联调 SOP"));
    await waitFor(() =>
      expect(updateMutate).toHaveBeenCalledWith(
        expect.objectContaining({ id: "doc-9", issue_id: "issue-1" }),
        expect.anything(),
      ),
    );
  });

  // Offering a document that is already here would be a no-op that looks like
  // an action.
  it("does not offer documents already attached", async () => {
    const user = userEvent.setup();
    render(
      [doc({ id: "doc-1", title: "已经挂了的" })],
      [
        doc({ id: "doc-1", title: "已经挂了的" }),
        doc({ id: "doc-9", title: "还没挂的" }),
      ],
    );
    await user.click(
      await screen.findByRole("button", { name: "Link a document" }),
    );
    expect(await screen.findByText("还没挂的")).toBeInTheDocument();
    // The attached one still shows in the list behind the dialog, so count it:
    // one occurrence means the row, not a second entry in the picker.
    expect(screen.getAllByText("已经挂了的")).toHaveLength(1);
  });

  // Detaching is not deleting — the document is untouched, only its issue_id.
  it("detaching sends null", async () => {
    const user = userEvent.setup();
    render([doc({ id: "doc-1" })]);
    await user.click(
      await screen.findByRole("button", { name: "Detach document" }),
    );
    await waitFor(() =>
      expect(updateMutate).toHaveBeenCalledWith(
        expect.objectContaining({ id: "doc-1", issue_id: null }),
        expect.anything(),
      ),
    );
  });
});
