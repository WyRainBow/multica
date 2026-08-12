import type { Card } from "@multica/core/types";

/** One day's worth of cards, newest day first and newest card first within it. */
export interface CardDayGroup {
  /** Local calendar day as `YYYY-MM-DD`; also the React key. */
  day: string;
  /** The instant the day starts, for formatting the heading. */
  date: Date;
  cards: Card[];
}

/**
 * Local calendar day of a timestamp, as `YYYY-MM-DD`.
 *
 * Built from the local getters rather than `toISOString().slice(0, 10)`: the
 * ISO string is UTC, so anything written after 08:00 in UTC+8 — most of a
 * working day — would be filed under tomorrow.
 */
function localDay(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

/**
 * Group cards into calendar days for the timeline.
 *
 * Days come back newest first, and cards within a day newest first, matching
 * the order the server already returns — a feed is read downwards into the
 * past. Sorting here rather than trusting the input keeps the grouping honest
 * if a caller hands over a differently ordered list.
 *
 * Cards with an unparseable timestamp are dropped rather than bucketed under
 * "Invalid Date": a heading no one can read is worse than a missing row, and
 * the card is still reachable from search.
 */
export function groupCardsByDay(cards: readonly Card[]): CardDayGroup[] {
  const byDay = new Map<string, { date: Date; cards: Card[] }>();

  for (const card of cards) {
    const created = new Date(card.created_at);
    if (Number.isNaN(created.getTime())) continue;
    const day = localDay(created);
    let group = byDay.get(day);
    if (!group) {
      // Midnight local time, so the heading formats the right calendar day
      // regardless of when in that day the first card happened to land.
      const start = new Date(created);
      start.setHours(0, 0, 0, 0);
      group = { date: start, cards: [] };
      byDay.set(day, group);
    }
    group.cards.push(card);
  }

  return [...byDay.entries()]
    .map(([day, group]) => ({
      day,
      date: group.date,
      cards: [...group.cards].sort(
        (a, b) =>
          new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
      ),
    }))
    .sort((a, b) => b.date.getTime() - a.date.getTime());
}
