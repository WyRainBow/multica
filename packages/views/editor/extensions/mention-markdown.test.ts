import { describe, it, expect } from "vitest";
import { BaseMentionExtension } from "./mention-extension";

const renderMarkdown = BaseMentionExtension.config.renderMarkdown as (
  node: { attrs: Record<string, string> },
) => string;

/**
 * What the picker leaves in the text once something is chosen.
 *
 * The rendered form is the transport: the readonly renderer resolves it back
 * into a chip, and a type it does not carry is a reference that cannot be
 * written at all — which is what a wiki page was, until the picker learned it.
 */
describe("BaseMentionExtension.renderMarkdown", () => {
  it("writes a wiki page as a reference, not as an address", () => {
    const out = renderMarkdown({
      attrs: { type: "doc", id: "b14cbd2f", label: "Agent 工作树协作" },
    });
    expect(out).toBe("[Agent 工作树协作](mention://doc/b14cbd2f)");
    // @ marks a person. A page is being referred to, not addressed — and the
    // stray @ would end up in the rendered chip's fallback label.
    expect(out).not.toContain("@");
  });

  it("keeps addressing people with @", () => {
    const out = renderMarkdown({
      attrs: { type: "member", id: "u-1", label: "cocoyu" },
    });
    expect(out).toBe("[@cocoyu](mention://member/u-1)");
  });

  it("leaves issues and projects unaddressed, as before", () => {
    for (const type of ["issue", "project"]) {
      const out = renderMarkdown({ attrs: { type, id: "x", label: "L" } });
      expect(out, `${type} should not be addressed with @`).toBe(
        `[L](mention://${type}/x)`,
      );
    }
  });
});
