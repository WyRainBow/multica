import { describe, it, expect } from "vitest";
import type { IssueResource } from "@multica/core/types";
import { resourceHost, resourceLabel } from "./resource-label";

function resource(overrides: Partial<IssueResource> = {}): IssueResource {
  return {
    id: "r-1",
    workspace_id: "ws-1",
    issue_id: "i-1",
    url: "https://example.com/page",
    title: "",
    author_type: "member",
    author_id: "u-1",
    position: 0,
    created_at: "2026-08-06T00:00:00Z",
    updated_at: "2026-08-06T00:00:00Z",
    ...overrides,
  };
}

describe("resourceLabel", () => {
  it("prefers the title someone typed", () => {
    expect(resourceLabel(resource({ title: "智能纪要：沟通会" }))).toBe("智能纪要：沟通会");
  });

  it("trims the title", () => {
    expect(resourceLabel(resource({ title: "  有标题  " }))).toBe("有标题");
  });

  // Host alone is not enough: three Feishu docs would render as three
  // identical rows reading "feishu.cn".
  it("falls back to host and path when untitled", () => {
    expect(resourceLabel(resource({ url: "https://example.feishu.cn/docx/abc123" }))).toBe(
      "example.feishu.cn/docx/abc123",
    );
  });

  it("drops a trailing slash from the fallback", () => {
    expect(resourceLabel(resource({ url: "https://example.com/team/" }))).toBe(
      "example.com/team",
    );
  });

  it("uses the host alone when the path carries nothing", () => {
    expect(resourceLabel(resource({ url: "https://example.com/" }))).toBe("example.com");
    expect(resourceLabel(resource({ url: "https://example.com" }))).toBe("example.com");
  });

  // The server only accepts http(s), but an older row or a drifting backend
  // could still hand the UI something unparseable — a row must render rather
  // than throw inside the list.
  it("falls back to the raw string when the url will not parse", () => {
    expect(resourceLabel(resource({ url: "not a url" }))).toBe("not a url");
  });
});

describe("resourceHost", () => {
  it("returns the host", () => {
    expect(resourceHost("https://example.feishu.cn/docx/abc")).toBe("example.feishu.cn");
  });

  it("keeps a port, which distinguishes two local services", () => {
    expect(resourceHost("http://localhost:3000/x")).toBe("localhost:3000");
  });

  // Empty rather than throwing: the caller hides the host column when it is
  // empty, and a thrown error would take the whole section down.
  it("returns empty for an unparseable url", () => {
    expect(resourceHost("not a url")).toBe("");
  });
});
