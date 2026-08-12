import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Card, Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const getCard = vi.hoisted(() => vi.fn());
const listCards = vi.hoisted(() => vi.fn());
const getIssue = vi.hoisted(() => vi.fn());
const updateMutate = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { getCard, listCards, getIssue },
  dispatchReasonCode: () => undefined,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    docs: () => "/acme/docs",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("@multica/core/docs/mutations", () => ({
  useUpdateCard: () => ({ mutate: updateMutate }),
  useDeleteCard: () => ({ mutate: vi.fn() }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    children,
    href,
  }: {
    children: React.ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("../../editor", () => ({
  ContentEditor: () => <div data-testid="editor" />,
}));

vi.mock("../../issues/components/description-outline", () => ({
  DescriptionOutline: () => null,
}));

// Stand in for the real command palette: it searches server-side, which this
// test is not exercising. What matters is that selecting an issue reaches the
// mutation.
vi.mock("../../modals/issue-picker-modal", () => ({
  IssuePickerModal: ({
    open,
    onSelect,
  }: {
    open: boolean;
    onSelect: (issue: Issue) => void;
  }) =>
    open ? (
      <button
        type="button"
        onClick={() => onSelect({ id: "issue-7" } as Issue)}
      >
        pick COC-7
      </button>
    ) : null,
}));

import { DocDetail } from "./doc-detail";

function card(overrides: Partial<Card> = {}): Card {
  return {
    id: "doc-1",
    workspace_id: "ws-1",
    issue_id: null,
    title: "本地起 P0 workflow 全链路 SOP",
    content: "body",
    kind: "联调",
    created_at: "2026-08-11T00:00:00Z",
    updated_at: "2026-08-11T00:00:00Z",
    ...overrides,
  } as unknown as Card;
}

function render(doc: Card) {
  getCard.mockResolvedValue(doc);
  listCards.mockResolvedValue({ cards: [doc] });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DocDetail docId="doc-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  updateMutate.mockClear();
  getIssue.mockReset();
});

// The link existed in the API, the CLI, and the issue page's document section
// from the start — but nothing in the app could SET it. So that section, which
// renders nothing when empty, was empty for everyone who does not use the CLI.
describe("linking a document to its issue", () => {
  it("offers the link control when the document has no issue", async () => {
    render(card());
    expect(
      await screen.findByRole("button", { name: "Link an issue" }),
    ).toBeInTheDocument();
  });

  it("picking an issue writes issue_id", async () => {
    const user = userEvent.setup();
    render(card());
    await user.click(
      await screen.findByRole("button", { name: "Link an issue" }),
    );
    await user.click(await screen.findByRole("button", { name: "pick COC-7" }));
    await waitFor(() =>
      expect(updateMutate).toHaveBeenCalledWith(
        expect.objectContaining({ id: "doc-1", issue_id: "issue-7" }),
        expect.anything(),
      ),
    );
  });

  // An explicit null detaches; omitting the field would leave the link in place.
  it("unlinking sends null, not an omitted field", async () => {
    const user = userEvent.setup();
    getIssue.mockResolvedValue({
      id: "issue-7",
      identifier: "COC-7",
      title: "把文档接进 issue",
    } as unknown as Issue);
    render(card({ issue_id: "issue-7" } as Partial<Card>));
    await user.click(
      await screen.findByRole("button", { name: "Unlink issue" }),
    );
    await waitFor(() =>
      expect(updateMutate).toHaveBeenCalledWith(
        expect.objectContaining({ id: "doc-1", issue_id: null }),
        expect.anything(),
      ),
    );
  });

  it("shows the linked issue's key and title", async () => {
    getIssue.mockResolvedValue({
      id: "issue-7",
      identifier: "COC-7",
      title: "把文档接进 issue",
    } as unknown as Issue);
    render(card({ issue_id: "issue-7" } as Partial<Card>));
    expect(await screen.findByText("COC-7")).toBeInTheDocument();
    expect(await screen.findByText("把文档接进 issue")).toBeInTheDocument();
  });

  // The issue list is paginated and drops archived issues, so the old lookup
  // rendered nothing for a link that was actually stored. Fetching by id keeps
  // the link visible even before the row arrives.
  it("still shows a link whose issue has not loaded", async () => {
    getIssue.mockImplementation(() => new Promise(() => {}));
    render(card({ issue_id: "issue-7" } as Partial<Card>));
    expect(await screen.findByText("Linked issue")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Link an issue" }),
    ).not.toBeInTheDocument();
  });
});

// The body preview on the list is height-capped and the detail page shows the
// text itself, so neither said how much there is. It is also the number an
// agent reads, which makes it the answer to "how much am I handing over".
describe("document length", () => {
  it("shows the saved length on open", async () => {
    render(card({ content: "x".repeat(11674) } as Partial<Card>));
    expect(await screen.findByText("11674 chars")).toBeInTheDocument();
  });

  // An empty document showing "0 chars" is a label with nothing to label.
  it("shows nothing for an empty document", async () => {
    render(card({ content: "" } as Partial<Card>));
    await screen.findByTestId("editor");
    expect(screen.queryByText(/chars/)).not.toBeInTheDocument();
  });
});
