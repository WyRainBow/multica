import { describe, expect, it } from "vitest";
import { createStore } from "zustand/vanilla";
import { viewStoreSlice, type IssueViewState } from "./view-store";

function makeStore() {
  return createStore<IssueViewState>()((set) => viewStoreSlice(set));
}

describe("view store archive visibility", () => {
  it("hides archived issues by default", () => {
    expect(makeStore().getState().showArchived).toBe(false);
  });

  it("toggles independently of the sub-issue switch", () => {
    // The two are different questions — "is this a child" and "is this still
    // in view" — so one must never move the other.
    const store = makeStore();
    store.getState().toggleShowArchived();
    expect(store.getState().showArchived).toBe(true);
    expect(store.getState().showSubIssues).toBe(true);

    store.getState().toggleShowSubIssues();
    expect(store.getState().showArchived).toBe(true);
    expect(store.getState().showSubIssues).toBe(false);
  });

  it("is not cleared by Reset filters", () => {
    // Archived-visibility is a display switch, not a filter chip; clearing the
    // filter bar must not silently flood the board with archived cards.
    const store = makeStore();
    store.getState().toggleShowArchived();
    store.getState().clearFilters();
    expect(store.getState().showArchived).toBe(true);
  });
});
