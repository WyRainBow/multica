import { describe, it, expect } from "vitest";
import { IssueResourceSchema, IssueResourceListResponseSchema } from "./schemas";

// A resource row is rendered as a clickable link on someone's issue page. The
// schema is what stands between a drifting backend and a blank section, so the
// cases that matter are the malformed ones.

describe("IssueResourceSchema", () => {
  it("parses a well-formed resource", () => {
    const parsed = IssueResourceSchema.parse({
      id: "r-1",
      workspace_id: "ws-1",
      issue_id: "i-1",
      url: "https://example.feishu.cn/docx/abc",
      title: "智能纪要",
      author_type: "agent",
      author_id: "a-1",
      position: 1000,
      created_at: "2026-08-06T00:00:00Z",
      updated_at: "2026-08-06T00:00:00Z",
    });
    expect(parsed.url).toBe("https://example.feishu.cn/docx/abc");
    expect(parsed.title).toBe("智能纪要");
  });

  // An older backend predates title/author/position. The row still has to
  // render — it falls back to the host — so those fields default rather than
  // failing the whole list.
  it("fills in the fields an older backend would omit", () => {
    const parsed = IssueResourceSchema.parse({
      id: "r-1",
      workspace_id: "ws-1",
      issue_id: "i-1",
      url: "https://example.com/x",
      created_at: "2026-08-06T00:00:00Z",
      updated_at: "2026-08-06T00:00:00Z",
    });
    expect(parsed.title).toBe("");
    expect(parsed.author_type).toBe("member");
    expect(parsed.position).toBe(0);
  });

  // Identity fields have no sensible default: a row without a url is not a
  // degraded row, it is not a row.
  it("rejects a resource with no url", () => {
    expect(() =>
      IssueResourceSchema.parse({
        id: "r-1",
        workspace_id: "ws-1",
        issue_id: "i-1",
        created_at: "2026-08-06T00:00:00Z",
        updated_at: "2026-08-06T00:00:00Z",
      }),
    ).toThrow();
  });

  // Unknown fields must pass through, not fail: a newer backend adding a field
  // must not blank the section on an installed desktop build.
  it("tolerates fields it has never seen", () => {
    const parsed = IssueResourceSchema.parse({
      id: "r-1",
      workspace_id: "ws-1",
      issue_id: "i-1",
      url: "https://example.com/x",
      created_at: "2026-08-06T00:00:00Z",
      updated_at: "2026-08-06T00:00:00Z",
      favicon_url: "https://example.com/favicon.ico",
    });
    expect(parsed.url).toBe("https://example.com/x");
  });
});

describe("IssueResourceListResponseSchema", () => {
  it("defaults a missing list to empty rather than failing", () => {
    expect(IssueResourceListResponseSchema.parse({}).resources).toEqual([]);
  });
});
