import { describe, it, expect } from "vitest";
import { IssueNamespaceSchema, IssueNamespaceSlotSchema } from "./schemas";
import { parseWithFallback } from "./schema";
import type { IssueNamespace } from "../types";

// The directory is the one read that can see placeholder cards — every other
// card read filters them out in SQL. So a slot that fails to parse does not
// just lose a row, it turns "nobody has written the design doc" back into
// "there is no design doc", which is the exact confusion the feature exists to
// remove. The malformed cases are the ones that matter.

function slot(overrides: Record<string, unknown> = {}) {
  return {
    name: "requirements",
    label: "需求底稿",
    kind: "COC-338/requirements",
    type: "document",
    exists: true,
    placeholder: true,
    card_id: "11111111-1111-1111-1111-111111111111",
    title: "COC-338 需求底稿（待补）",
    count: 0,
    ...overrides,
  };
}

describe("IssueNamespaceSlotSchema", () => {
  it("parses a well-formed slot", () => {
    const parsed = IssueNamespaceSlotSchema.parse(slot());
    expect(parsed.name).toBe("requirements");
    expect(parsed.label).toBe("需求底稿");
    expect(parsed.type).toBe("document");
    expect(parsed.placeholder).toBe(true);
  });

  // `card_id` and `title` carry `omitempty` on the wire: a folder whose
  // placeholder is gone but which has documents beneath it sends neither.
  it("fills in the omitempty fields the server drops", () => {
    const parsed = IssueNamespaceSlotSchema.parse({
      name: "rounds",
      label: "评审轮次",
      kind: "COC-338/rounds",
      type: "folder",
      exists: true,
      placeholder: false,
      count: 3,
    });
    expect(parsed.card_id).toBe("");
    expect(parsed.title).toBe("");
  });

  // The name is the stable key the row is matched and keyed by. A slot without
  // one is not a degraded slot, it is not a slot.
  it("rejects a slot with no name", () => {
    expect(() => IssueNamespaceSlotSchema.parse(slot({ name: undefined }))).toThrow();
  });

  // A type this build has never heard of must render as a row, not vanish —
  // the UI switch has a `default` branch waiting for exactly this.
  it("keeps a slot type it has never seen", () => {
    expect(IssueNamespaceSlotSchema.parse(slot({ type: "index" })).type).toBe("index");
  });

  // A newer backend adding a field must not blank the section on an installed
  // desktop build.
  it("tolerates fields it has never seen", () => {
    const parsed = IssueNamespaceSlotSchema.parse(
      slot({ frozen_at: "2026-08-24T00:00:00Z" }),
    );
    expect(parsed.name).toBe("requirements");
  });
});

describe("IssueNamespaceSchema", () => {
  it("parses the whole directory in wire order", () => {
    const parsed = IssueNamespaceSchema.parse({
      issue_id: "22222222-2222-2222-2222-222222222222",
      key: "COC-338",
      root: "COC-338",
      slots: [slot(), slot({ name: "design", label: "技术方案" })],
    });
    expect(parsed.slots.map((s) => s.name)).toEqual(["requirements", "design"]);
  });

  it("defaults a missing slot list to empty rather than failing", () => {
    expect(IssueNamespaceSchema.parse({}).slots).toEqual([]);
  });
});

// The contract that actually protects the page: whatever comes back, the
// caller gets a value and the section renders.
describe("GET /api/issues/:id/namespace through parseWithFallback", () => {
  const opts = { endpoint: "GET /api/issues/:id/namespace" };
  // Explicit type argument, matching the client: the fallback for this
  // endpoint is `null`, and TypeScript would otherwise narrow the anchor type
  // down to `null` alone and lose the shape a successful parse returns.
  const parse = (data: unknown) =>
    parseWithFallback<IssueNamespace | null>(data, IssueNamespaceSchema, null, opts);

  it("returns the parsed directory when the response is well-formed", () => {
    const parsed = parse({
      issue_id: "i-1",
      key: "COC-338",
      root: "COC-338",
      slots: [slot()],
    });
    expect(parsed?.slots[0]?.label).toBe("需求底稿");
  });

  it("falls back instead of throwing when slots is not an array", () => {
    expect(
      parse({ issue_id: "i-1", key: "COC-338", root: "COC-338", slots: "requirements" }),
    ).toBeNull();
  });

  it("falls back instead of throwing when a slot has the wrong types", () => {
    expect(
      parse({ issue_id: "i-1", slots: [slot({ count: "three", placeholder: "yes" })] }),
    ).toBeNull();
  });

  it("falls back instead of throwing on a non-object response", () => {
    expect(parse("not the directory")).toBeNull();
  });

  // Missing top-level fields degrade rather than fail: an issue whose key did
  // not come back still shows the six slots it did.
  it("still returns a directory when the top-level fields are missing", () => {
    const parsed = parse({ slots: [slot()] });
    expect(parsed?.key).toBe("");
    expect(parsed?.slots).toHaveLength(1);
  });
});
