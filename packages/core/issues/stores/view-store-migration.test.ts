import { describe, it, expect } from "vitest";
import {
  DEFAULT_TABLE_COLUMNS,
  VIEW_STORE_VERSION,
  viewStorePersistOptions,
} from "./view-store";

const migrate = viewStorePersistOptions("test").migrate;

// Persisted state beats defaults — that is what persistence is for. So editing
// DEFAULT_TABLE_COLUMNS reaches nobody who has opened the page before unless a
// version bump carries it across. This is the test that makes the next such
// change fail loudly instead of silently doing nothing.
describe("view store migration", () => {
  const old = {
    viewMode: "board" as const,
    tableColumns: [
      { key: "title", width: 360 },
      { key: "priority", width: 130 },
    ],
    statusFilters: ["todo"],
    sortBy: "priority" as const,
    tableCollapsedGroups: ["done"],
  };

  it("moves an old snapshot to the table view", () => {
    expect(migrate(old, 1).viewMode).toBe("table");
  });

  it("replaces the stored columns with the current defaults", () => {
    expect(migrate(old, 1).tableColumns?.map((c) => c.key)).toEqual(
      DEFAULT_TABLE_COLUMNS.map((c) => c.key),
    );
  });

  // The layout is what the version is about. Dropping the whole snapshot would
  // take filters, sort and collapsed groups with it — a far bigger loss than
  // the layout it set out to fix.
  it("keeps everything the version is not about", () => {
    const migrated = migrate(old, 1);
    expect(migrated.statusFilters).toEqual(["todo"]);
    expect(migrated.sortBy).toBe("priority");
    expect(migrated.tableCollapsedGroups).toEqual(["done"]);
  });

  // Someone who already migrated has since chosen their own columns; running
  // it again would throw their choice away on every page load.
  it("leaves a current snapshot untouched", () => {
    const chosen = { ...old, viewMode: "board" as const };
    expect(migrate(chosen, VIEW_STORE_VERSION)).toEqual(chosen);
  });

  it("survives an empty snapshot", () => {
    expect(migrate(undefined, 0).viewMode).toBe("table");
  });
});
