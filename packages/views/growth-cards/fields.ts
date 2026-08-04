import type { GrowthCard, GrowthCardFields } from "@multica/core/types";

/**
 * The seven body fields of a growth card, in the order they are asked.
 *
 * The order is the method, not a layout choice: what you did not know comes
 * before the plan you were given, which comes before what you confirmed you
 * understood, which comes before what you verified yourself. Reading a card
 * top to bottom should show where the loop stopped.
 *
 * `title` (需求) is deliberately not in this list — it is the card's name and
 * gets its own single-line input rather than a textarea.
 */
export const GROWTH_CARD_BODY_KEYS = [
  "systems",
  "unknowns",
  "agent_plan",
  "understood",
  "verified",
  "learned",
  "next_gaps",
] as const;

export type GrowthCardBodyKey = (typeof GROWTH_CARD_BODY_KEYS)[number];

/** Every writable key, title first. */
export const GROWTH_CARD_KEYS = ["title", ...GROWTH_CARD_BODY_KEYS] as const;

/**
 * The body fields this card actually has something in.
 *
 * Blanks are dropped rather than rendered as empty rows: a card is meant to be
 * saved half-written, and seven grey placeholders would make every honest card
 * look broken.
 */
export function filledBodyFields(
  card: GrowthCard,
): Array<{ key: GrowthCardBodyKey; value: string }> {
  const filled: Array<{ key: GrowthCardBodyKey; value: string }> = [];
  for (const key of GROWTH_CARD_BODY_KEYS) {
    const value = card[key]?.trim();
    if (value) filled.push({ key, value });
  }
  return filled;
}

/** How much of the loop this card records, for the list's progress hint. */
export function filledCount(card: GrowthCard): number {
  return GROWTH_CARD_BODY_KEYS.filter((key) => card[key]?.trim()).length;
}

/** Free-text haystack for the list's client-side search. */
export function searchHaystack(card: GrowthCard): string {
  return GROWTH_CARD_KEYS.map((key) => card[key] ?? "")
    .join("\n")
    .toLowerCase();
}

/** A blank draft, used when opening the editor for a new card. */
export function emptyDraft(): Required<GrowthCardFields> {
  return {
    title: "",
    systems: "",
    unknowns: "",
    agent_plan: "",
    understood: "",
    verified: "",
    learned: "",
    next_gaps: "",
  };
}

/** The card's stored values as an editable draft. */
export function draftFromCard(card: GrowthCard): Required<GrowthCardFields> {
  return {
    title: card.title ?? "",
    systems: card.systems ?? "",
    unknowns: card.unknowns ?? "",
    agent_plan: card.agent_plan ?? "",
    understood: card.understood ?? "",
    verified: card.verified ?? "",
    learned: card.learned ?? "",
    next_gaps: card.next_gaps ?? "",
  };
}
