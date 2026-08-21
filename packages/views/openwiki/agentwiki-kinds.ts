import type { Card } from "@multica/core/types";

/**
 * The Agent Wiki shelves, derived from the documents rather than declared.
 *
 * The page used to render one hardcoded shelf, `AgentWiki/cases/`. Everything
 * else filed under AgentWiki was invisible in both directions: the wiki tab
 * excludes the whole prefix, and this tab only asked for cases — so a playbook
 * written through the CLI existed, could be read by an agent, and appeared on
 * no page at all. Naming four shelves instead of one would only move that
 * failure to the fifth.
 *
 * So the shelf is whatever segment follows the prefix. Known ones lead, in the
 * order the distillation loop produces them; anything new sorts in after,
 * labelled by its own name, visible the moment it exists.
 */
const PREFIX = "AgentWiki/";

const KNOWN = ["cases", "patterns", "playbooks", "assets"];

export interface AgentWikiShelf {
  /** The path segment after the prefix — "cases", "playbooks", … */
  slug: string;
  cards: Card[];
}

/** The segment naming the shelf, or "" for a card that is not on one. */
export function shelfSlug(kind: string | null | undefined): string {
  const value = (kind ?? "").trim();
  if (!value.startsWith(PREFIX)) return "";
  return value.slice(PREFIX.length).split("/")[0] ?? "";
}

export function agentWikiShelves(cards: Card[]): AgentWikiShelf[] {
  const bySlug = new Map<string, Card[]>();
  for (const card of cards) {
    const slug = shelfSlug(card.kind);
    // A document filed as exactly "AgentWiki/" names no shelf. Dropping it
    // would repeat the bug this function exists to end, so it gets one.
    const key = slug === "" ? "" : slug;
    if (!card.kind?.startsWith(PREFIX)) continue;
    const bucket = bySlug.get(key) ?? [];
    bucket.push(card);
    bySlug.set(key, bucket);
  }

  return [...bySlug.entries()]
    .map(([slug, shelfCards]) => ({
      slug,
      cards: [...shelfCards].sort((a, b) =>
        b.updated_at.localeCompare(a.updated_at),
      ),
    }))
    .sort((a, b) => {
      const ai = KNOWN.indexOf(a.slug);
      const bi = KNOWN.indexOf(b.slug);
      if (ai !== -1 && bi !== -1) return ai - bi;
      if (ai !== -1) return -1;
      if (bi !== -1) return 1;
      return a.slug.localeCompare(b.slug);
    });
}
