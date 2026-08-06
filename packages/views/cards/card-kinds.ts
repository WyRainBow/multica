import type { Card } from "@multica/core/types";

/** A tab on the cards page. `kind` is "" for the全部 tab, which has no filter. */
export interface CardKindTab {
  kind: string;
  count: number;
}

/**
 * The tabs, derived from the cards themselves rather than from a fixed list.
 *
 * Kinds are free text, so the set that exists is whatever has been written.
 * A fixed enum would make renaming a tab a migration, and would show a
 * category nobody has filed anything under — a tab that cannot be filled is
 * worse than no tab.
 *
 * Ordered most-used first so a category filed into daily does not sit behind
 * one tried once, then by name so the order is stable between two kinds with
 * the same count.
 *
 * Uncategorised cards get no tab of their own: 全部 already shows them, and a
 * blank label has nothing to render. They are still reachable by leaving 全部
 * selected.
 */
export function cardKindTabs(cards: readonly Card[]): CardKindTab[] {
  const counts = new Map<string, number>();
  for (const card of cards) {
    const kind = card.kind?.trim() ?? "";
    if (!kind) continue;
    counts.set(kind, (counts.get(kind) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([kind, count]) => ({ kind, count }))
    .sort((a, b) => b.count - a.count || a.kind.localeCompare(b.kind));
}

/**
 * The cards a tab shows. An empty `kind` is the 全部 tab and filters nothing —
 * distinct from a card whose own kind is empty, which 全部 also shows.
 */
export function filterCardsByKind(cards: readonly Card[], kind: string): Card[] {
  if (!kind) return [...cards];
  return cards.filter((card) => (card.kind?.trim() ?? "") === kind);
}
