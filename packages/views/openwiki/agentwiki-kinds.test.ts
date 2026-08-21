import { describe, it, expect } from "vitest";
import type { Card } from "@multica/core/types";
import { agentWikiShelves, shelfSlug } from "./agentwiki-kinds";

function card(kind: string, id = kind, updated = "2026-08-20T00:00:00Z"): Card {
  return { id, title: id, kind, updated_at: updated } as unknown as Card;
}

describe("shelfSlug", () => {
  it("reads the segment after the prefix, however deep the kind goes", () => {
    expect(shelfSlug("AgentWiki/cases/")).toBe("cases");
    expect(shelfSlug("AgentWiki/playbooks/retro")).toBe("playbooks");
  });

  it("claims nothing outside the prefix", () => {
    expect(shelfSlug("指南/Multica使用")).toBe("");
    expect(shelfSlug(null)).toBe("");
  });
});

describe("agentWikiShelves", () => {
  it("shows a shelf nobody declared", () => {
    // The bug: a playbook written through the CLI existed and appeared on no
    // page, because the tab asked for cases and the wiki excludes the prefix.
    const shelves = agentWikiShelves([
      card("AgentWiki/cases/"),
      card("AgentWiki/playbooks/"),
      card("AgentWiki/checklists/"),
    ]);
    expect(shelves.map((s) => s.slug)).toEqual([
      "cases",
      "playbooks",
      "checklists",
    ]);
  });

  it("leads with the known shelves and sorts the rest by name", () => {
    const shelves = agentWikiShelves([
      card("AgentWiki/zebra/"),
      card("AgentWiki/assets/"),
      card("AgentWiki/alpha/"),
      card("AgentWiki/cases/"),
    ]);
    expect(shelves.map((s) => s.slug)).toEqual([
      "cases",
      "assets",
      "alpha",
      "zebra",
    ]);
  });

  it("leaves documents outside the prefix alone", () => {
    expect(agentWikiShelves([card("指南/Multica使用")])).toEqual([]);
  });

  it("puts the newest document at the top of its shelf", () => {
    const shelves = agentWikiShelves([
      card("AgentWiki/cases/", "old", "2026-01-01T00:00:00Z"),
      card("AgentWiki/cases/", "new", "2026-08-01T00:00:00Z"),
    ]);
    expect(shelves[0]?.cards.map((c) => c.id)).toEqual(["new", "old"]);
  });
});
