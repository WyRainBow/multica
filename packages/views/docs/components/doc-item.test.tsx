import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Card } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCards from "../../locales/en/docs.json";
import { DocItem } from "./doc-item";

function renderCard(card: Card) {
  return render(
    <I18nProvider locale="en" resources={{ en: { docs: enCards } }}>
      <DocItem card={card} onEdit={vi.fn()} onOpenIssue={vi.fn()} />
    </I18nProvider>,
  );
}

// The renderer itself is covered in packages/views/rich-content; here it only
// needs to be reachable, so the stub records what it was handed.
vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
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
