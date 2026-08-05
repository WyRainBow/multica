import { describe, it, expect } from "vitest";
import type { Card } from "@multica/core/types";
import { groupCardsByDay } from "./group-by-day";

function card(id: string, createdAt: string): Card {
  return {
    id,
    workspace_id: "ws-1",
    issue_id: null,
    author_type: "member",
    author_id: "user-1",
    title: id,
    content: "",
    created_at: createdAt,
    updated_at: createdAt,
  };
}

// Local time throughout: the timeline is read by a person in one timezone, and
// the heading has to say the day they wrote it.
describe("groupCardsByDay", () => {
  it("puts cards written on the same local day in one group", () => {
    const groups = groupCardsByDay([
      card("a", "2026-08-05T09:07:00"),
      card("b", "2026-08-05T21:30:00"),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0]!.cards.map((c) => c.id)).toEqual(["b", "a"]);
  });

  it("orders days newest first", () => {
    const groups = groupCardsByDay([
      card("older", "2026-08-03T10:00:00"),
      card("newer", "2026-08-05T10:00:00"),
      card("middle", "2026-08-04T10:00:00"),
    ]);
    expect(groups.map((g) => g.cards[0]!.id)).toEqual([
      "newer",
      "middle",
      "older",
    ]);
  });

  it("orders cards inside a day newest first", () => {
    const groups = groupCardsByDay([
      card("morning", "2026-08-05T09:00:00"),
      card("evening", "2026-08-05T20:00:00"),
      card("noon", "2026-08-05T12:00:00"),
    ]);
    expect(groups[0]!.cards.map((c) => c.id)).toEqual([
      "evening",
      "noon",
      "morning",
    ]);
  });

  it("does not depend on the order it was handed", () => {
    const ordered = groupCardsByDay([
      card("new", "2026-08-05T10:00:00"),
      card("old", "2026-08-01T10:00:00"),
    ]);
    const shuffled = groupCardsByDay([
      card("old", "2026-08-01T10:00:00"),
      card("new", "2026-08-05T10:00:00"),
    ]);
    expect(shuffled.map((g) => g.day)).toEqual(ordered.map((g) => g.day));
  });

  it("keys a day by its local calendar date", () => {
    const groups = groupCardsByDay([card("a", "2026-08-05T09:07:00")]);
    expect(groups[0]!.day).toBe("2026-08-05");
  });

  // The day's start, not the first card's timestamp — the heading formats a
  // calendar day and must not drift with whenever the first card landed.
  it("anchors the group at local midnight", () => {
    const groups = groupCardsByDay([card("a", "2026-08-05T23:59:00")]);
    const date = groups[0]!.date;
    expect(date.getHours()).toBe(0);
    expect(date.getMinutes()).toBe(0);
    expect(date.getDate()).toBe(5);
  });

  // A card whose timestamp will not parse would otherwise create a group
  // headed "Invalid Date", which is worse than the row being absent.
  it("drops cards with an unreadable timestamp", () => {
    const groups = groupCardsByDay([
      card("good", "2026-08-05T10:00:00"),
      card("bad", "not a date"),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0]!.cards.map((c) => c.id)).toEqual(["good"]);
  });

  it("is empty for no cards", () => {
    expect(groupCardsByDay([])).toEqual([]);
  });
});
