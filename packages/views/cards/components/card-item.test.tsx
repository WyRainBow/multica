import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Card } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCards from "../../locales/en/cards.json";
import { CardItem } from "./card-item";

function renderCard(card: Card) {
  return render(
    <I18nProvider locale="en" resources={{ en: { cards: enCards } }}>
      <CardItem card={card} onEdit={vi.fn()} onOpenIssue={vi.fn()} />
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

describe("CardItem", () => {
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
