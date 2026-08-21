import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Card, Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCards from "../../locales/en/docs.json";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { DocItem } from "./doc-item";

function renderCard(card: Card) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  // useCardInvalidation resolves the workspace through the cached list;
  // seed it so the rename/delete mutations mount without a workspace route.
  qc.setQueryData(["workspaces", "list"], [
    { id: "ws-1", name: "Test", slug: "test-ws" },
  ]);
  return render(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="test-ws">
      <I18nProvider locale="en" resources={{ en: { docs: enCards } }}>
        <DocItem card={card} onEdit={vi.fn()} onOpenIssue={vi.fn()} />
      </I18nProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

// The renderer itself is covered in packages/views/rich-content; here it only
// needs to be reachable, so the stub records what it was handed.
vi.mock("../../rich-content", () => ({
  RichContent: ({ content }: { content: string }) => (
    <div data-testid="rendered-markdown">{content}</div>
  ),
}));

function makeCard(overrides: Partial<Card> = {}): Card {
  return {
    id: "card-1",
    workspace_id: "ws-1",
    issue_id: null,
    author_type: "member",
    author_id: "user-1",
    kind: "",
    title: "便宜的充值网站",
    content: "**待确认**（用之前先自己核实）",
    created_at: "2026-08-04T02:15:00Z",
    updated_at: "2026-08-04T02:15:00Z",
    ...overrides,
  };
}

describe("DocItem", () => {
  // The editor writes Markdown, so a card shown as raw source puts `**bold**`
  // and link syntax on screen — which is what it did before.
  it("hands the body to the Markdown renderer rather than printing it", () => {
    renderCard(makeCard());

    const rendered = screen.getByTestId("rendered-markdown");
    expect(rendered).toHaveTextContent("待确认");
  });

  it("renders no body block when the card is title-only", () => {
    renderCard(makeCard({ content: "   " }));

    expect(screen.queryByTestId("rendered-markdown")).not.toBeInTheDocument();
  });
});

// The body is cut at a fixed height, so a 300-character note and an
// 11674-character SOP look identical on the list. The count is the only thing
// that says how much is below the cut.
describe("DocItem length", () => {
  it("shows how long the document is", () => {
    renderCard(makeCard({ content: "x".repeat(11674) }));
    expect(screen.getByText("11674 chars")).toBeInTheDocument();
  });

  it("shows nothing for an empty document", () => {
    renderCard(makeCard({ content: "" }));
    expect(screen.queryByText(/chars/)).not.toBeInTheDocument();
  });

  // It sits next to the requirement chip, but a document with no requirement
  // still has a length — an earlier version of that row only rendered when a
  // requirement existed.
  it("shows it on a document with no requirement", () => {
    renderCard(makeCard({ issue_id: null, content: "12345" }));
    expect(screen.getByText("5 chars")).toBeInTheDocument();
  });
});

// The workspace issue list is the first pages only, so a document pointing at
// a finished issue found nothing there and the row claimed the issue was
// unavailable. It was not — it was past the page. Three states now, and only
// one of them says "gone".
describe("DocItem's linked issue", () => {
  function renderWith(props: { issue?: Issue; issueGone?: boolean }) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    qc.setQueryData(["workspaces", "list"], [
      { id: "ws-1", name: "Test", slug: "test-ws" },
    ]);
    return render(
      <QueryClientProvider client={qc}>
        <WorkspaceSlugProvider slug="test-ws">
          <I18nProvider locale="en" resources={{ en: { docs: enCards } }}>
            <DocItem
              card={makeCard({ issue_id: "issue-7" })}
              onEdit={vi.fn()}
              onOpenIssue={vi.fn()}
              {...props}
            />
          </I18nProvider>
        </WorkspaceSlugProvider>
      </QueryClientProvider>,
    );
  }

  const done = {
    id: "issue-7",
    identifier: "COC-23",
    status: "done",
  } as unknown as Issue;

  it("shows the key of a finished issue instead of calling it unavailable", () => {
    renderWith({ issue: done });
    expect(screen.getByText("COC-23")).toBeInTheDocument();
    expect(
      screen.queryByText(/gone|unavailable|已不可用/i),
    ).not.toBeInTheDocument();
  });

  // Written while the work is live, read long after it finished — whether that
  // has happened is the first thing you want to know.
  it("shows the issue's status, not only its key", () => {
    renderWith({ issue: done });
    // Scoped to the chip: the row's edit control is a lucide icon too, and an
    // unscoped query for one would pass without any status ever rendering.
    const chip = screen.getByText("COC-23").closest("button")!;
    expect(chip.querySelector("svg")).toBeInTheDocument();
  });

  // Saying "unavailable" while the answer is still in flight is the same wrong
  // message, one second later.
  it("says nothing while the issue is still being resolved", () => {
    renderWith({ issue: undefined, issueGone: false });
    expect(
      screen.queryByText("The linked issue is gone"),
    ).not.toBeInTheDocument();
  });

  it("says so once the issue is confirmed gone", () => {
    renderWith({ issue: undefined, issueGone: true });
    expect(screen.getByText("The linked issue is gone")).toBeInTheDocument();
  });
});

// Row-level rename and delete (COC-298): the affordances live on the row so
// neither requires walking into the detail page.
describe("DocItem rename and delete", () => {
  it("exposes rename and delete buttons alongside edit", () => {
    renderCard(makeCard());
    expect(screen.getByLabelText("Rename")).toBeInTheDocument();
    expect(screen.getByLabelText("Delete")).toBeInTheDocument();
  });

  it("renames inline: Enter closes the editor on the row", async () => {
    const user = userEvent.setup();
    renderCard(makeCard());
    await user.click(screen.getByLabelText("Rename"));
    const input = screen.getByLabelText("Rename title");
    await user.clear(input);
    await user.type(input, "新的标题{Enter}");
    // The row keeps the old title until the invalidated query lands; what
    // Enter must do is end the edit — no textbox left on the row.
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("cancel rename with Escape leaves the title untouched", async () => {
    const user = userEvent.setup();
    renderCard(makeCard());
    await user.click(screen.getByLabelText("Rename"));
    await user.type(screen.getByLabelText("Rename title"), "x{Escape}");
    expect(screen.getByText("便宜的充值网站")).toBeInTheDocument();
  });

  it("delete asks for confirmation before calling the API", async () => {
    const user = userEvent.setup();
    renderCard(makeCard());
    await user.click(screen.getByLabelText("Delete"));
    expect(
      screen.getByText(/确定删除这篇文档|delete this document/i),
    ).toBeInTheDocument();
  });
});
