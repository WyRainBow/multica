import { describe, it, expect } from "vitest";
import type { Card } from "@multica/core/types";
import { docKindTabs, filterDocsByKind } from "./doc-kinds";

function card(id: string, kind: string): Card {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: null,
    author_type: "member",
    author_id: "user-1",
    kind,
    title: id,
    content: "",
    created_at: "2026-08-06T00:00:00Z",
    updated_at: "2026-08-06T00:00:00Z",
  };
}

describe("docKindTabs", () => {
  // Most-used first: a category filed into daily must not sit behind one
  // tried once, which is what alphabetical order would do.
  it("orders by count, then by name", () => {
    const tabs = docKindTabs([
      card("a", "文档"),
      card("b", "想法"),
      card("c", "想法"),
      card("d", "踩坑"),
    ]);
    expect(tabs).toEqual([
      { kind: "想法", count: 2 },
      { kind: "文档", count: 1 },
      { kind: "踩坑", count: 1 },
    ]);
  });

  // Uncategorised gets no tab: 全部 already shows those cards, and a blank
  // label has nothing to render.
  it("gives uncategorised cards no tab of their own", () => {
    expect(docKindTabs([card("a", ""), card("b", "  ")])).toEqual([]);
  });

  // A kind typed with a stray space is the same category, not a second tab.
  it("treats surrounding whitespace as the same kind", () => {
    expect(docKindTabs([card("a", "文档"), card("b", " 文档 ")])).toEqual([
      { kind: "文档", count: 2 },
    ]);
  });

  it("returns nothing for an empty workspace", () => {
    expect(docKindTabs([])).toEqual([]);
  });
});

describe("filterDocsByKind", () => {
  it("keeps only that kind", () => {
    const cards = [card("a", "文档"), card("b", "想法"), card("c", "文档")];
    expect(filterDocsByKind(cards, "文档").map((c) => c.id)).toEqual(["a", "c"]);
  });

  // The 全部 tab has no filter. Distinct from a card whose own kind is empty,
  // which 全部 also shows — that distinction is why the empty string means
  // "no filter" here rather than "the uncategorised ones".
  it("returns everything for the all tab", () => {
    const cards = [card("a", "文档"), card("b", "")];
    expect(filterDocsByKind(cards, "").map((c) => c.id)).toEqual(["a", "b"]);
  });

  it("matches a kind stored with stray whitespace", () => {
    expect(filterDocsByKind([card("a", " 文档 ")], "文档").map((c) => c.id)).toEqual(["a"]);
  });
});
